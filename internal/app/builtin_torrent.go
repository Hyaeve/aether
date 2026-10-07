package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

func validateBuiltinMagnet(raw string) error {
	spec, err := torrent.TorrentSpecFromMagnetUri(strings.TrimSpace(raw))
	if err != nil || (spec.InfoHash == (metainfo.Hash{}) && !spec.InfoHashV2.Ok) {
		return errors.New("磁力链接无效")
	}
	return nil
}

func builtinTorrentConfig(output string) *torrent.ClientConfig {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = output
	cfg.ListenPort = 0
	cfg.NoDefaultPortForwarding = true
	cfg.NoUpload = true
	cfg.Seed = false
	cfg.DisableWebtorrent = true
	// Keep completion metadata out of the tree that is uploaded to the storage pool.
	cfg.DefaultStorage = checkedTorrentStorage{storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir: output, PieceCompletion: storage.NewMapPieceCompletion(),
	})}
	return cfg
}

type checkedTorrentStorage struct{ storage.ClientImplCloser }

func (s checkedTorrentStorage) OpenTorrent(ctx context.Context, info *metainfo.Info, hash metainfo.Hash) (storage.TorrentImpl, error) {
	if err := validateTorrentInfo(info); err != nil {
		return storage.TorrentImpl{}, err
	}
	return s.ClientImplCloser.OpenTorrent(ctx, info, hash)
}

func validateTorrentInfo(info *metainfo.Info) error {
	if !safeName(info.BestName()) || strings.ContainsAny(info.BestName(), "\x00\r\n") {
		return errors.New("种子名称无效")
	}
	var total int64
	files := info.UpvertedFiles()
	if len(files) > 10000 {
		return errors.New("种子文件数量超过 10000")
	}
	for _, f := range files {
		if f.Length < 0 || f.Length > builtinDownloadLimit-total {
			return errors.New("种子大小超过 100 GiB")
		}
		total += f.Length
		for _, part := range f.BestPath() {
			if !safeName(part) || strings.ContainsAny(part, "\x00\r\n") {
				return errors.New("种子文件路径无效")
			}
		}
	}
	if total == 0 {
		return errors.New("种子没有可下载的内容")
	}
	return nil
}

func downloadBuiltinTorrent(ctx context.Context, output, input string, fromFile bool) error {
	return downloadBuiltinTorrentWithConfig(ctx, input, fromFile, builtinTorrentConfig(output))
}

func downloadBuiltinTorrentWithConfig(ctx context.Context, input string, fromFile bool, cfg *torrent.ClientConfig) error {
	var spec *torrent.TorrentSpec
	var err error
	if fromFile {
		mi, e := metainfo.LoadFromFile(input)
		if e != nil {
			return errors.New("种子文件格式无效")
		}
		info, e := mi.UnmarshalInfo()
		if e != nil {
			return errors.New("种子元数据无效")
		}
		if e = validateTorrentInfo(&info); e != nil {
			return e
		}
		spec, err = torrent.TorrentSpecFromMetaInfoErr(mi)
	} else {
		spec, err = torrent.TorrentSpecFromMagnetUri(input)
	}
	if err != nil {
		return errors.New("种子或磁力链接无效")
	}
	client, err := torrent.NewClient(cfg)
	if err != nil {
		return errors.New("初始化 BT 下载器失败")
	}
	defer client.Close()
	spec.DisallowDataDownload = true
	spec.DisallowDataUpload = true
	task, _, err := client.AddTorrentSpec(spec)
	if err != nil {
		return errors.New("添加 BT 下载失败")
	}
	defer task.Drop()
	timer := time.NewTimer(10 * time.Minute)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errors.New("获取磁力元数据超时")
	case <-task.GotInfo():
	}
	if err := validateTorrentInfo(task.Info()); err != nil {
		return err
	}
	task.AllowDataDownload()
	task.DownloadAll()
	// BytesCompleted also counts unverified chunks; wait for verified pieces.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-task.Complete().On():
		return nil
	}
}
