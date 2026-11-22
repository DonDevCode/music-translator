// Package settings stores how the bot behaves in each Discord server.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Behavior is what the bot does with song links in a channel.
type Behavior string

const (
	// Auto translates every recognized link right away.
	Auto Behavior = "auto"
	// OnRequest adds a reaction and translates once someone clicks it.
	OnRequest Behavior = "on-request"
	// Off ignores links.
	Off Behavior = "off"
)

// Valid reports whether b is one of the known behaviors.
func (b Behavior) Valid() bool {
	return b == Auto || b == OnRequest || b == Off
}

// NewGuildDefault is the behavior of servers that never configured the bot
// and didn't have it before settings existed (see Store.Initialize). On
// Request is the default since it saves API quota.
const NewGuildDefault = OnRequest

// MaxChannels limits the channel overrides of a server.
const MaxChannels = 50

// ErrTooManyChannels is returned when a server already has MaxChannels
// channel overrides.
var ErrTooManyChannels = fmt.Errorf("a server can have at most %d channel settings", MaxChannels)

// Guild is the configuration of one server: a default behavior and
// overrides for single channels.
//
// The modes offered to admins are presets of it: Auto is Default Auto,
// On Request is Default OnRequest, Channel Bound is Default Off with the
// chosen channels set to Auto.
type Guild struct {
	Default  Behavior            `json:"default"`
	Channels map[string]Behavior `json:"channels,omitempty"`
}

// BehaviorFor returns the behavior in a channel. For threads, parentID is
// the channel the thread is in, whose override applies unless the thread
// has its own; otherwise parentID is "".
func (g Guild) BehaviorFor(channelID, parentID string) Behavior {
	if b, ok := g.Channels[channelID]; ok {
		return b
	}
	if b, ok := g.Channels[parentID]; parentID != "" && ok {
		return b
	}
	return g.Default
}

func (g Guild) clone() Guild {
	c := Guild{Default: g.Default}
	if len(g.Channels) > 0 {
		c.Channels = make(map[string]Behavior, len(g.Channels))
		for id, b := range g.Channels {
			c.Channels[id] = b
		}
	}
	return c
}

// fileData is the content of the settings file.
type fileData struct {
	// Initialized is set once the servers the bot was in before settings
	// existed were set to Auto, so that their behavior doesn't change.
	Initialized bool             `json:"initialized"`
	Guilds      map[string]Guild `json:"guilds"`
}

// Store keeps the settings of all servers in memory and writes them to a
// JSON file on every change. Changes are rare, so rewriting the whole file
// is fine.
type Store struct {
	path string // "" keeps settings in memory only

	mu   sync.RWMutex
	data fileData
}

// Open loads the settings from path, which doesn't have to exist yet. With
// an empty path nothing is saved.
func Open(path string) (*Store, error) {
	s := &Store{path: path, data: fileData{Guilds: map[string]Guild{}}}
	if path == "" {
		return s, nil
	}

	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading settings: %w", err)
	}
	if err := json.Unmarshal(content, &s.data); err != nil {
		return nil, fmt.Errorf("parsing settings %s: %w", path, err)
	}
	if s.data.Guilds == nil {
		s.data.Guilds = map[string]Guild{}
	}
	return s, nil
}

// Guild returns the settings of a server, or the defaults if it has none.
func (s *Store) Guild(guildID string) Guild {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if g, ok := s.data.Guilds[guildID]; ok {
		return g.clone()
	}
	return Guild{Default: NewGuildDefault}
}

// SetDefault sets the default behavior of a server.
func (s *Store) SetDefault(guildID string, b Behavior) (Guild, error) {
	return s.update(guildID, func(g *Guild) error {
		g.Default = b
		return nil
	})
}

// SetChannel sets the behavior of one channel of a server.
func (s *Store) SetChannel(guildID, channelID string, b Behavior) (Guild, error) {
	return s.update(guildID, func(g *Guild) error {
		if _, ok := g.Channels[channelID]; !ok && len(g.Channels) >= MaxChannels {
			return ErrTooManyChannels
		}
		if g.Channels == nil {
			g.Channels = map[string]Behavior{}
		}
		g.Channels[channelID] = b
		return nil
	})
}

// RemoveChannel removes the override of a channel, if it has one.
func (s *Store) RemoveChannel(guildID, channelID string) (Guild, error) {
	s.mu.RLock()
	_, ok := s.data.Guilds[guildID].Channels[channelID]
	s.mu.RUnlock()
	if !ok {
		return s.Guild(guildID), nil
	}
	return s.update(guildID, func(g *Guild) error {
		delete(g.Channels, channelID)
		return nil
	})
}

// DeleteGuild removes all settings of a server, e.g. after the bot was
// removed from it.
func (s *Store) DeleteGuild(guildID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.data.Guilds[guildID]
	if !ok {
		return nil
	}
	delete(s.data.Guilds, guildID)
	if err := s.save(); err != nil {
		s.data.Guilds[guildID] = g
		return err
	}
	return nil
}

// Initialize sets all given servers without settings to Auto the first time
// it is called, so that servers that had the bot before settings existed
// keep its behavior. Later calls do nothing, so servers that join afterwards
// get NewGuildDefault.
func (s *Store) Initialize(guildIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Initialized {
		return nil
	}

	var added []string
	for _, id := range guildIDs {
		if _, ok := s.data.Guilds[id]; !ok {
			s.data.Guilds[id] = Guild{Default: Auto}
			added = append(added, id)
		}
	}
	s.data.Initialized = true
	if err := s.save(); err != nil {
		for _, id := range added {
			delete(s.data.Guilds, id)
		}
		s.data.Initialized = false
		return err
	}
	return nil
}

// update changes the settings of a server with fn and saves them. If saving
// fails, the change is undone.
func (s *Store) update(guildID string, fn func(*Guild) error) (Guild, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	old, existed := s.data.Guilds[guildID]
	g := Guild{Default: NewGuildDefault}
	if existed {
		g = old.clone()
	}
	if err := fn(&g); err != nil {
		return old.clone(), err
	}

	s.data.Guilds[guildID] = g
	if err := s.save(); err != nil {
		if existed {
			s.data.Guilds[guildID] = old
		} else {
			delete(s.data.Guilds, guildID)
		}
		return old.clone(), err
	}
	return g.clone(), nil
}

// save writes the settings atomically, so that a crash while writing
// doesn't leave a broken file. s.mu must be held.
func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	content, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding settings: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating settings directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("saving settings: %w", err)
	}
	defer os.Remove(tmp.Name()) // fails harmlessly after the rename
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("saving settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("saving settings: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("saving settings: %w", err)
	}
	return nil
}
