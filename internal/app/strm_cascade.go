package app

import (
	"context"
	"path"
	"regexp"
	"strings"
)

var strmGroupSidecar = regexp.MustCompile(`^(?:tvshow|movie|season[0-9]*|poster|fanart|landscape|clearlogo|banner|thumb|clearart|disc)(?:-[a-z0-9]+)*$`)

func strmCompanion(name, media string) bool {
	if path.Dir(name) != path.Dir(media) {
		return false
	}
	stem := strings.TrimSuffix(name, path.Ext(name))
	plain := scrapeMediaStem(media)
	full := strings.TrimSuffix(media, path.Ext(media))
	for _, base := range []string{plain, full} {
		if strings.EqualFold(stem, base) {
			return true
		}
		for _, suffix := range []string{"-poster", "-fanart", "-thumb", "-banner", "-landscape", "-clearlogo", "-disc", "-clearart"} {
			if strings.EqualFold(stem, base+suffix) {
				return true
			}
		}
		if strings.HasPrefix(strings.ToLower(stem), strings.ToLower(base)+".") {
			return true
		}
	}
	return false
}

func (h *strmTaskSync) queueCascade(name string) error {
	if !h.task.CascadeDelete || !strings.EqualFold(path.Ext(name), ".strm") {
		return nil
	}
	for _, existing := range h.manifest.PendingCascade {
		if existing == name {
			return nil
		}
	}
	h.manifest.PendingCascade = append(h.manifest.PendingCascade, name)
	return h.save()
}

func (h *strmTaskSync) cascadeOutputs(ctx context.Context, removed []string) error {
	for _, name := range removed {
		if err := h.queueCascade(name); err != nil {
			return err
		}
	}
	return h.deleteOutputs(ctx, nil)
}

func (h *strmTaskSync) planCascade(ctx context.Context, gone map[string]bool) ([]strmDeleteCandidate, error) {
	if !h.task.CascadeDelete {
		return nil, nil
	}
	pending := map[string]bool{}
	for _, name := range h.manifest.PendingCascade {
		if strings.EqualFold(path.Ext(name), ".strm") {
			pending[name] = true
		}
	}
	for name := range gone {
		if strings.EqualFold(path.Ext(name), ".strm") {
			pending[name] = true
		}
	}
	extensions := h.task.CascadeExtensions
	if extensions == "" {
		extensions = defaultSTRMCascadeExtensions
	}
	if err := h.check(ctx); err != nil {
		return nil, err
	}
	entries, err := scanQuarkLibraryDisk(ctx, h.root)
	if err != nil {
		return nil, err
	}
	media := []string{}
	for _, entry := range entries {
		if !entry.info.IsDir() && !gone[entry.name] && strings.EqualFold(path.Ext(entry.name), ".strm") {
			media = append(media, entry.name)
		}
	}
	candidates := []strmDeleteCandidate{}
	for _, entry := range entries {
		if entry.info.IsDir() || h.wanted[entry.name] || !excludedType(entry.name, extensions) {
			continue
		}
		group := strmGroupSidecar.MatchString(strings.ToLower(strings.TrimSuffix(path.Base(entry.name), path.Ext(entry.name))))
		matched := false
		for removed := range pending {
			if !group && strmCompanion(entry.name, removed) {
				matched = true
			}
			dir := path.Dir(entry.name)
			if group && (dir == "." || path.Dir(removed) == dir || strings.HasPrefix(removed, dir+"/")) {
				matched = true
			}
		}
		if !matched {
			continue
		}
		for _, live := range media {
			dir := path.Dir(entry.name)
			if group && (dir == "." || path.Dir(live) == dir || strings.HasPrefix(live, dir+"/")) || !group && strmCompanion(entry.name, live) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		digest, identity, err := strmOutputDigest(h.root, entry.name)
		if err != nil {
			return nil, err
		}
		if registered := h.manifest.Files[entry.name]; registered != "" && registered != digest {
			continue
		}
		candidates = append(candidates, strmDeleteCandidate{entry.name, digest, identity})
	}
	return candidates, nil
}
