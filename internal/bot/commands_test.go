package bot

import (
	"reflect"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/DonDevCode/music-translator/internal/settings"
)

func TestMissingPermissions(t *testing.T) {
	base := int64(discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionReadMessageHistory)

	tests := []struct {
		name     string
		perms    int64
		behavior settings.Behavior
		want     []string
	}{
		{"auto ok", base, settings.Auto, nil},
		{"on request needs reactions", base, settings.OnRequest, []string{"Add Reactions"}},
		{"on request ok", base | discordgo.PermissionAddReactions, settings.OnRequest, nil},
		{"nothing", 0, settings.Auto, []string{"View Channel", "Send Messages", "Read Message History"}},
		{"administrator", discordgo.PermissionAdministrator, settings.OnRequest, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := missingPermissions(tt.perms, tt.behavior); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("missingPermissions = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasOwnReaction(t *testing.T) {
	msg := func(reactions ...*discordgo.MessageReactions) *discordgo.Message {
		return &discordgo.Message{Reactions: reactions}
	}
	ours := &discordgo.MessageReactions{Me: true, Emoji: &discordgo.Emoji{Name: requestEmoji}}
	others := &discordgo.MessageReactions{Count: 3, Emoji: &discordgo.Emoji{Name: requestEmoji}}
	custom := &discordgo.MessageReactions{Me: true, Emoji: &discordgo.Emoji{Name: requestEmoji, ID: "123"}}

	if !hasOwnReaction(msg(others, ours), requestEmoji) {
		t.Error("own reaction not found")
	}
	if hasOwnReaction(msg(others), requestEmoji) {
		t.Error("other users' reaction counted as own")
	}
	if hasOwnReaction(msg(custom), requestEmoji) {
		t.Error("custom emoji with the same name counted")
	}
	if hasOwnReaction(msg(), requestEmoji) {
		t.Error("no reactions counted")
	}
}

func TestModeCommands(t *testing.T) {
	store, _ := settings.Open("")
	b := &Bot{settings: store}

	if reply := b.setMode("g", modeChannelBound); !strings.Contains(reply, "No channels are chosen") {
		t.Errorf("Channel Bound without channels: reply %q lacks hint", reply)
	}
	if got := store.Guild("g").Default; got != settings.Off {
		t.Errorf("Channel Bound default = %q, want off", got)
	}

	b.setChannel("g", "music", settings.Auto)
	if reply := b.setMode("g", modeChannelBound); strings.Contains(reply, "No channels are chosen") {
		t.Errorf("Channel Bound with channels: reply %q has hint", reply)
	}
	if got := store.Guild("g").BehaviorFor("music", ""); got != settings.Auto {
		t.Errorf("chosen channel behavior = %q, want auto", got)
	}

	b.setMode("g", modeOnRequest)
	b.removeChannel("g", "music")
	if got := store.Guild("g").BehaviorFor("music", ""); got != settings.OnRequest {
		t.Errorf("behavior after removing channel = %q, want on-request", got)
	}
}

func TestCommandsValid(t *testing.T) {
	for _, c := range commands() {
		if c.Type == discordgo.MessageApplicationCommand {
			continue
		}
		var walk func(opts []*discordgo.ApplicationCommandOption)
		walk = func(opts []*discordgo.ApplicationCommandOption) {
			for _, o := range opts {
				if len(o.Description) > 100 {
					t.Errorf("option %s: description longer than 100 characters", o.Name)
				}
				for _, ch := range o.Choices {
					if len(ch.Name) > 100 {
						t.Errorf("choice %q longer than 100 characters", ch.Name)
					}
				}
				walk(o.Options)
			}
		}
		walk(c.Options)
	}
}
