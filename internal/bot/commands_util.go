package bot

import (
	"maps"
	"slices"

	"github.com/bwmarrin/discordgo"

	"github.com/DonDevCode/music-translator/internal/settings"
)

// requiredPermissions are the permissions needed to answer links with a
// behavior, with their names as shown in Discord.
var requiredPermissions = []struct {
	perm     int64
	name     string
	behavior settings.Behavior // "" if needed for all behaviors
}{
	{discordgo.PermissionViewChannel, "View Channel", ""},
	{discordgo.PermissionSendMessages, "Send Messages", ""},
	// Needed to reply to messages and to add reactions.
	{discordgo.PermissionReadMessageHistory, "Read Message History", ""},
	{discordgo.PermissionAddReactions, "Add Reactions", settings.OnRequest},
}

// missingPermissions returns the names of the permissions in
// requiredPermissions that perms lacks for behavior.
func missingPermissions(perms int64, behavior settings.Behavior) []string {
	if perms&discordgo.PermissionAdministrator != 0 {
		return nil
	}
	var missing []string
	for _, p := range requiredPermissions {
		if (p.behavior == "" || p.behavior == behavior) && perms&p.perm == 0 {
			missing = append(missing, p.name)
		}
	}
	return missing
}

// describeMode describes a server's default behavior as one of the modes
// admins choose from.
func describeMode(b settings.Behavior) string {
	switch b {
	case settings.Auto:
		return "**Auto**: I translate every song link."
	case settings.OnRequest:
		return "**On Request**: I add " + requestEmoji + " to song links and translate them when someone clicks it."
	}
	return "**Channel Bound**: I only answer in the channels chosen with `/translator channel set`."
}

func describeBehavior(b settings.Behavior) string {
	switch b {
	case settings.Auto:
		return "Auto"
	case settings.OnRequest:
		return "On Request"
	}
	return "Off"
}

// hasBehavior reports whether any channel of g is set to b.
func hasBehavior(g settings.Guild, b settings.Behavior) bool {
	for _, cb := range g.Channels {
		if cb == b {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
