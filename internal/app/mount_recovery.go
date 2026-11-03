package app

import (
	"errors"
	"strings"
)

type fuseMountIdentity struct{ ID, Point, Kind, Source string }

func parseFuseMountIdentity(data, point string) (fuseMountIdentity, bool, error) {
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	var found fuseMountIdentity
	present := false
	for _, line := range strings.Split(data, "\n") {
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}
		left, right := strings.Fields(parts[0]), strings.Fields(parts[1])
		if len(left) < 6 || len(right) < 2 || unescape.Replace(left[4]) != point {
			continue
		}
		if present {
			return found, true, errors.New("挂载点有多层挂载，不能自动恢复，请核对宿主机")
		}
		present = true
		found = fuseMountIdentity{left[0], point, right[0], unescape.Replace(right[1])}
	}
	return found, present, nil
}

func ownFuseMount(entry fuseMountIdentity, mount MountConfig) bool {
	return entry.Point == mountTarget(mount) && entry.Kind == "fuse.aether" && entry.Source == "aether:"+mount.Name
}
