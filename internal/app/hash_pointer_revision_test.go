package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRemoteED2KHashAndLength(t *testing.T) {
	for _, size := range []int64{2, 3, 4} {
		info, err := ed2kRemoteInfo(context.Background(), File{Name: "film.mkv", Size: size}, strings.NewReader("abc"))
		if size == 3 {
			if err != nil || info.Hash != "a448017aaf21d8525fc10ae87aa6729d" || info.Size != 3 {
				t.Fatal(info, err)
			}
		} else if err == nil {
			t.Fatal("accepted stale size", size)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ed2kRemoteInfo(ctx, File{Size: 3}, strings.NewReader("abc")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestED2KSourceBindingTypes(t *testing.T) {
	a, source, _, task, _ := ed2kFixture(t)
	for _, kind := range []string{"local", "115", "mobile", "tianyi", "quark"} {
		source.Type = kind
		_, err := a.validateED2KBinding(task, source)
		if (err == nil) != (kind == "local" || kind == "115") {
			t.Fatal(kind, err)
		}
	}
}

func TestCASSourceBindingMatrix(t *testing.T) {
	a := testApp(t)
	bindings := addCASBindings(t, a)
	for _, source := range []string{"local", "mobile", "tianyi", "115"} {
		for _, binding := range bindings {
			_, err := a.casBinding(Task{CASBindingID: binding.ID}, Storage{Type: source})
			if (err == nil) != (source == "local" || source == binding.Type) {
				t.Fatal(source, binding.Type, err)
			}
		}
	}
}
