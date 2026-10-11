package app

import (
	"bytes"
	"context"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const backupManifestRecordLimit = 1 << 20

type backupManifestRecord struct {
	Kind                     string
	Storage                  Storage
	File                     File
	Relative, Parent, Digest string
	From, To                 int
	Name                     string
	Remove, Directory        bool
}

// The manifest owns only its encrypted file, never the scanner's source slice.
// Run and Close serialize; a cancelled waiter does not retain a queue payload.
type backupManifest[T any] struct {
	file          *os.File
	root          *os.Root
	name          string
	aead          cipher.AEAD
	header        []byte
	count         int
	size          int64
	gate          chan struct{}
	closed        bool
	pending       []T
	peak, refills int
}

func encodeBackupManifest[T any](item T) ([]byte, error) {
	var record backupManifestRecord
	switch value := any(item).(type) {
	case backupEntry:
		record = backupManifestRecord{Kind: "backup", Storage: value.source, File: value.file, Relative: value.relative, Parent: value.parent}
	case syncOperation:
		record = backupManifestRecord{Kind: "sync", Storage: value.file.source, File: value.file.file, Relative: value.file.relative, Parent: value.file.parent, Digest: value.file.digest, From: value.from, To: value.to, Name: value.name, Remove: value.remove, Directory: value.directory}
	default:
		return nil, errors.New("unsupported backup manifest descriptor")
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	if len(raw) > backupManifestRecordLimit {
		return nil, errors.New("backup manifest record too large")
	}
	return raw, nil
}

func decodeBackupManifest[T any](raw []byte) (T, error) {
	var zero T
	var record backupManifestRecord
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return zero, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return zero, errors.New("invalid manifest record trailing data")
	}
	entry := backupEntry{source: record.Storage, file: record.File, relative: record.Relative, parent: record.Parent}
	var value any
	switch any(zero).(type) {
	case backupEntry:
		if record.Kind != "backup" {
			return zero, errors.New("manifest descriptor type mismatch")
		}
		value = entry
	case syncOperation:
		if record.Kind != "sync" {
			return zero, errors.New("manifest descriptor type mismatch")
		}
		value = syncOperation{from: record.From, to: record.To, name: record.Name, file: syncFile{backupEntry: entry, digest: record.Digest}, remove: record.Remove, directory: record.Directory}
	default:
		return zero, errors.New("unsupported backup manifest descriptor")
	}
	return value.(T), nil
}

func newBackupManifest[T any](ctx context.Context, a *App, items []T) (*backupManifest[T], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(a.dataDir)
	if err != nil {
		return nil, err
	}
	name := ".aether-backup-queue-" + id() + ".tmp"
	f, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		root.Close()
		return nil, err
	}
	m := &backupManifest[T]{file: f, root: root, name: name, aead: a.store.aead, count: len(items), gate: make(chan struct{}, 1)}
	ok := false
	defer func() {
		if !ok {
			_ = m.Close()
		}
	}()
	m.header = make([]byte, 40)
	copy(m.header, "ABQUEUE1")
	binary.BigEndian.PutUint64(m.header[8:16], uint64(len(items)))
	if _, err = rand.Read(m.header[16:]); err != nil {
		return nil, err
	}
	if _, err = f.Write(m.header); err != nil {
		return nil, err
	}
	for index, item := range items {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		raw, encodeErr := encodeBackupManifest(item)
		if encodeErr != nil {
			return nil, encodeErr
		}
		nonce := make([]byte, m.aead.NonceSize())
		if _, err = rand.Read(nonce); err != nil {
			return nil, err
		}
		sealed := m.aead.Seal(nonce, nonce, raw, m.aad(index))
		clear(raw)
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(sealed)))
		if _, err = f.Write(length[:]); err != nil {
			return nil, err
		}
		if _, err = f.Write(sealed); err != nil {
			return nil, err
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = f.Sync(); err != nil {
		return nil, err
	}
	m.size, err = f.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	ok = true
	return m, nil
}

func (m *backupManifest[T]) aad(index int) []byte {
	aad := make([]byte, len(m.header)+8)
	copy(aad, m.header)
	binary.BigEndian.PutUint64(aad[len(m.header):], uint64(index))
	return aad
}

func (m *backupManifest[T]) reader() (*io.SectionReader, error) {
	info, err := m.file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() != m.size {
		return nil, errors.New("backup manifest size changed")
	}
	r := io.NewSectionReader(m.file, 0, m.size)
	header := make([]byte, len(m.header))
	if _, err = io.ReadFull(r, header); err != nil {
		return nil, err
	}
	if !bytes.Equal(header, m.header) {
		return nil, errors.New("backup manifest header changed")
	}
	return r, nil
}

func (m *backupManifest[T]) read(ctx context.Context, r io.Reader, index int) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	var length [4]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		return zero, err
	}
	n := int64(binary.BigEndian.Uint32(length[:]))
	overhead := int64(m.aead.NonceSize() + m.aead.Overhead())
	if n < overhead || n > backupManifestRecordLimit+overhead {
		return zero, errors.New("invalid backup manifest record length")
	}
	sealed := make([]byte, int(n))
	if _, err := io.ReadFull(r, sealed); err != nil {
		return zero, err
	}
	raw, err := m.aead.Open(nil, sealed[:m.aead.NonceSize()], sealed[m.aead.NonceSize():], m.aad(index))
	if err != nil {
		return zero, errors.New("backup manifest authentication failed")
	}
	defer clear(raw)
	return decodeBackupManifest[T](raw)
}

func (m *backupManifest[T]) Run(ctx context.Context, config BackupResources, consume func(T) error) error {
	select {
	case m.gate <- struct{}{}:
		defer func() { <-m.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.closed {
		return os.ErrClosed
	}
	if consume == nil {
		return errors.New("missing backup consumer")
	}
	if config.QueueUpper < 0 || config.QueueLower < 0 || config.QueueUpper == 0 && config.QueueLower != 0 || config.QueueUpper > 0 && config.QueueLower >= config.QueueUpper {
		return errors.New("invalid backup queue limits")
	}
	r, err := m.reader()
	if err != nil {
		return err
	}
	// Verify the complete preflight before any consumer can mutate a destination.
	for index := 0; index < m.count; index++ {
		if _, err = m.read(ctx, r, index); err != nil {
			return fmt.Errorf("verify backup manifest: %w", err)
		}
	}
	position, err := r.Seek(0, io.SeekCurrent)
	if err != nil || position != m.size {
		return errors.New("trailing backup manifest data")
	}
	r, err = m.reader()
	if err != nil {
		return err
	}
	upper := config.QueueUpper
	if upper == 0 {
		upper = max(1, m.count)
	}
	upper = min(upper, max(1, m.count))
	lower := config.QueueLower
	if lower == 0 {
		lower = upper / 2
	}
	lower = min(lower, upper-1)
	m.pending = make([]T, upper)
	m.peak, m.refills = 0, 0
	defer func() { clear(m.pending); m.pending = nil }()
	head, queued, next := 0, 0, 0
	for queued > 0 || next < m.count {
		if err = ctx.Err(); err != nil {
			return err
		}
		if queued <= lower && next < m.count {
			m.refills++
			for queued < upper && next < m.count {
				item, readErr := m.read(ctx, r, next)
				if readErr != nil {
					return readErr
				}
				m.pending[(head+queued)%upper] = item
				queued++
				next++
			}
			m.peak = max(m.peak, queued)
		}
		item := m.pending[head]
		var zero T
		m.pending[head] = zero
		head = (head + 1) % upper
		queued--
		if err = consume(item); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (m *backupManifest[T]) Close() error {
	m.gate <- struct{}{}
	defer func() { <-m.gate }()
	if m.closed {
		return nil
	}
	m.closed = true
	clear(m.pending)
	m.pending = nil
	closeErr := m.file.Close()
	removeErr := m.root.Remove(m.name)
	rootErr := m.root.Close()
	return errors.Join(closeErr, removeErr, rootErr)
}
