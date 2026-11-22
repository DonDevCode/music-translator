package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestBehaviorFor(t *testing.T) {
	g := Guild{Default: Off, Channels: map[string]Behavior{"music": Auto, "thread": OnRequest}}

	tests := []struct {
		name, channel, parent string
		want                  Behavior
	}{
		{"default", "general", "", Off},
		{"override", "music", "", Auto},
		{"thread inherits parent", "t1", "music", Auto},
		{"thread override wins", "thread", "music", OnRequest},
		{"thread in channel without override", "t1", "general", Off},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := g.BehaviorFor(tt.channel, tt.parent); got != tt.want {
				t.Errorf("BehaviorFor(%q, %q) = %q, want %q", tt.channel, tt.parent, got, tt.want)
			}
		})
	}
}

func TestStorePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "settings.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	if g := s.Guild("g1"); g.Default != NewGuildDefault {
		t.Errorf("unconfigured guild default = %q, want %q", g.Default, NewGuildDefault)
	}
	if _, err := s.SetDefault("g1", Off); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetChannel("g1", "c1", Auto); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetChannel("g2", "c2", Auto); err != nil {
		t.Fatal(err)
	}

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if g := s.Guild("g1"); g.Default != Off || g.Channels["c1"] != Auto {
		t.Errorf("reloaded g1 = %+v", g)
	}
	if g := s.Guild("g2"); g.Default != NewGuildDefault || g.Channels["c2"] != Auto {
		t.Errorf("reloaded g2 = %+v", g)
	}

	if _, err := s.RemoveChannel("g1", "c1"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGuild("g2"); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if g := s.Guild("g1"); len(g.Channels) != 0 {
		t.Errorf("channel override survived removal: %+v", g)
	}
	if g := s.Guild("g2"); len(g.Channels) != 0 {
		t.Errorf("guild survived deletion: %+v", g)
	}
}

func TestGuildIsCopy(t *testing.T) {
	s, _ := Open("")
	s.SetChannel("g", "c", Auto)
	g := s.Guild("g")
	g.Channels["c"] = Off
	if s.Guild("g").Channels["c"] != Auto {
		t.Error("changing a returned Guild changed the store")
	}
}

func TestInitialize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, _ := Open(path)
	s.SetDefault("configured", Off)

	if err := s.Initialize([]string{"old", "configured"}); err != nil {
		t.Fatal(err)
	}
	if got := s.Guild("old").Default; got != Auto {
		t.Errorf("existing guild default = %q, want auto", got)
	}
	if got := s.Guild("configured").Default; got != Off {
		t.Errorf("configured guild default = %q, want off", got)
	}

	// Guilds the bot joins later get the new default, also after a restart.
	s, _ = Open(path)
	if err := s.Initialize([]string{"old", "new"}); err != nil {
		t.Fatal(err)
	}
	if got := s.Guild("new").Default; got != NewGuildDefault {
		t.Errorf("new guild default = %q, want %q", got, NewGuildDefault)
	}
}

func TestMaxChannels(t *testing.T) {
	s, _ := Open("")
	for i := range MaxChannels {
		if _, err := s.SetChannel("g", fmt.Sprint(i), Auto); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SetChannel("g", "one more", Auto); !errors.Is(err, ErrTooManyChannels) {
		t.Errorf("err = %v, want ErrTooManyChannels", err)
	}
	if _, err := s.SetChannel("g", "0", Off); err != nil {
		t.Errorf("changing an existing override at the limit: %v", err)
	}
}

func TestSaveFailureKeepsOldSettings(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(filepath.Join(dir, "settings.json"))
	s.SetDefault("g", Off)

	// A directory where the file should be makes the rename fail.
	os.Remove(filepath.Join(dir, "settings.json"))
	os.Mkdir(filepath.Join(dir, "settings.json"), 0o755)
	if _, err := s.SetDefault("g", Auto); err == nil {
		t.Fatal("expected error")
	}
	if got := s.Guild("g").Default; got != Off {
		t.Errorf("default after failed save = %q, want off", got)
	}
}
