package bot

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/DonDevCode/music-translator/internal/settings"
)

// translateCommandName is the message context menu command (right-click a
// message, Apps) that translates its links in any mode.
const translateCommandName = "Translate song links"

// The values of the mode option. Channel Bound is stored as the default
// behavior Off, with the chosen channels set to Auto.
const (
	modeAuto         = "auto"
	modeOnRequest    = "on-request"
	modeChannelBound = "channel-bound"
)

// commands returns the bot's application commands.
func commands() []*discordgo.ApplicationCommand {
	// Discord only shows /translator to members who can manage the server.
	// Admins can allow other roles in Server Settings > Integrations, so the
	// handler doesn't check permissions itself.
	manageGuild := int64(discordgo.PermissionManageGuild)
	guildOnly := &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild}

	behaviorOption := &discordgo.ApplicationCommandOption{
		Type:        discordgo.ApplicationCommandOptionString,
		Name:        "behavior",
		Description: "What to do with song links there (default: Auto)",
		Choices: []*discordgo.ApplicationCommandOptionChoice{
			{Name: "Auto: translate right away", Value: string(settings.Auto)},
			{Name: "On Request: translate when someone clicks the reaction", Value: string(settings.OnRequest)},
			{Name: "Off: ignore song links", Value: string(settings.Off)},
		},
	}
	channelOption := &discordgo.ApplicationCommandOption{
		Type:        discordgo.ApplicationCommandOptionChannel,
		Name:        "channel",
		Description: "The channel; threads in it use its setting",
		Required:    true,
		ChannelTypes: []discordgo.ChannelType{
			discordgo.ChannelTypeGuildText,
			discordgo.ChannelTypeGuildNews,
			discordgo.ChannelTypeGuildForum,
			discordgo.ChannelTypeGuildVoice,
			discordgo.ChannelTypeGuildPublicThread,
			discordgo.ChannelTypeGuildPrivateThread,
			discordgo.ChannelTypeGuildNewsThread,
		},
	}

	return []*discordgo.ApplicationCommand{
		{
			Name:                     "translator",
			Description:              "Choose how the bot answers song links in this server",
			DefaultMemberPermissions: &manageGuild,
			Contexts:                 guildOnly,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "mode",
					Description: "Set the mode for the whole server",
					Options: []*discordgo.ApplicationCommandOption{{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "mode",
						Description: "The mode",
						Required:    true,
						Choices: []*discordgo.ApplicationCommandOptionChoice{
							{Name: "Auto: translate every song link", Value: modeAuto},
							{Name: "On Request: add a reaction, translate when clicked", Value: modeOnRequest},
							{Name: "Channel Bound: translate only in chosen channels", Value: modeChannelBound},
						},
					}},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommandGroup,
					Name:        "channel",
					Description: "Settings for single channels",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:        discordgo.ApplicationCommandOptionSubCommand,
							Name:        "set",
							Description: "Set what the bot does in a channel, regardless of the mode",
							Options:     []*discordgo.ApplicationCommandOption{channelOption, behaviorOption},
						},
						{
							Type:        discordgo.ApplicationCommandOptionSubCommand,
							Name:        "remove",
							Description: "Make a channel follow the server's mode again",
							Options:     []*discordgo.ApplicationCommandOption{channelOption},
						},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "status",
					Description: "Show the mode, channel settings and missing permissions",
				},
			},
		},
		{
			Type:     discordgo.MessageApplicationCommand,
			Name:     translateCommandName,
			Contexts: guildOnly,
		},
	}
}

// registerCommands replaces the bot's global commands with the current
// ones, so restarts don't create duplicates.
func (b *Bot) registerCommands(s *discordgo.Session, appID string) {
	if _, err := s.ApplicationCommandBulkOverwrite(appID, "", commands()); err != nil {
		log.Printf("Registering commands: %v", err)
	}
}

func (b *Bot) onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand || i.GuildID == "" {
		return
	}
	data := i.ApplicationCommandData()
	switch data.Name {
	case "translator":
		b.handleTranslatorCommand(s, i, data)
	case translateCommandName:
		b.handleTranslateCommand(s, i, data)
	}
}

func (b *Bot) handleTranslatorCommand(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	if len(data.Options) == 0 {
		return
	}
	sub := data.Options[0]

	var reply string
	switch sub.Name {
	case "mode":
		reply = b.setMode(i.GuildID, sub.Options[0].StringValue())
	case "channel":
		if len(sub.Options) == 0 {
			return
		}
		action := sub.Options[0]
		opts := optionMap(action.Options)
		channelID := opts["channel"].Value.(string)
		if action.Name == "set" {
			behavior := settings.Auto
			if o, ok := opts["behavior"]; ok {
				behavior = settings.Behavior(o.StringValue())
			}
			reply = b.setChannel(i.GuildID, channelID, behavior)
		} else {
			reply = b.removeChannel(i.GuildID, channelID)
		}
	case "status":
		reply = b.status(s, i)
	default:
		return
	}
	respondEphemeral(s, i, reply)
}

func (b *Bot) setMode(guildID, mode string) string {
	behavior := settings.Auto
	switch mode {
	case modeOnRequest:
		behavior = settings.OnRequest
	case modeChannelBound:
		behavior = settings.Off
	}
	g, err := b.settings.SetDefault(guildID, behavior)
	if err != nil {
		log.Printf("Saving mode of server %s: %v", guildID, err)
		return "Couldn't save the setting, please try again later."
	}

	reply := "Mode set to " + describeMode(g.Default)
	if g.Default == settings.Off && !hasBehavior(g, settings.Auto) && !hasBehavior(g, settings.OnRequest) {
		reply += "\nNo channels are chosen yet, so I won't answer anywhere. Choose them with `/translator channel set`."
	}
	return reply
}

func (b *Bot) setChannel(guildID, channelID string, behavior settings.Behavior) string {
	if !behavior.Valid() {
		return "Unknown behavior."
	}
	if _, err := b.settings.SetChannel(guildID, channelID, behavior); err != nil {
		if errors.Is(err, settings.ErrTooManyChannels) {
			return fmt.Sprintf("This server already has %d channel settings. Remove one with `/translator channel remove` first.", settings.MaxChannels)
		}
		log.Printf("Saving channel setting of server %s: %v", guildID, err)
		return "Couldn't save the setting, please try again later."
	}
	return fmt.Sprintf("<#%s> is set to %s.", channelID, describeBehavior(behavior))
}

func (b *Bot) removeChannel(guildID, channelID string) string {
	g, err := b.settings.RemoveChannel(guildID, channelID)
	if err != nil {
		log.Printf("Removing channel setting of server %s: %v", guildID, err)
		return "Couldn't save the setting, please try again later."
	}
	return fmt.Sprintf("<#%s> follows the server's mode again: %s.", channelID, describeBehavior(g.Default))
}

// status describes the settings of the server and the permissions the bot
// lacks in channels where it should answer.
func (b *Bot) status(s *discordgo.Session, i *discordgo.InteractionCreate) string {
	g := b.settings.Guild(i.GuildID)

	var sb strings.Builder
	sb.WriteString("**Mode:** " + describeMode(g.Default))
	if len(g.Channels) > 0 {
		sb.WriteString("\n\n**Channels:**")
		for _, id := range sortedKeys(g.Channels) {
			fmt.Fprintf(&sb, "\n<#%s>: %s", id, describeBehavior(g.Channels[id]))
		}
	} else if g.Default == settings.Off {
		sb.WriteString("\nNo channels are chosen yet. Choose them with `/translator channel set`.")
	}

	// Channels where the bot should answer. The current channel stands in
	// for all others when the mode applies everywhere.
	var problems []string
	checked := map[string]bool{}
	check := func(channelID string, behavior settings.Behavior, perms int64, known bool) {
		if behavior == settings.Off || checked[channelID] {
			return
		}
		checked[channelID] = true
		if !known {
			var err error
			if perms, err = s.UserChannelPermissions(s.State.User.ID, channelID); err != nil {
				problems = append(problems, fmt.Sprintf("<#%s>: can't see the channel", channelID))
				return
			}
		}
		if missing := missingPermissions(perms, behavior); len(missing) > 0 {
			problems = append(problems, fmt.Sprintf("<#%s>: %s", channelID, strings.Join(missing, ", ")))
		}
	}
	check(i.ChannelID, b.behaviorFor(s, i.GuildID, i.ChannelID), i.AppPermissions, true)
	for _, id := range sortedKeys(g.Channels) {
		check(id, g.Channels[id], 0, false)
	}
	if len(problems) > 0 {
		sb.WriteString("\n\n**Missing permissions:**\n" + strings.Join(problems, "\n"))
	}

	sb.WriteString("\n\nAnyone can also right-click a message and choose Apps > " + translateCommandName + ", in every mode.")
	return sb.String()
}

// handleTranslateCommand translates the links of a message on request,
// regardless of the server's mode.
func (b *Bot) handleTranslateCommand(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	if data.Resolved == nil || data.Resolved.Messages[data.TargetID] == nil {
		return
	}
	m := data.Resolved.Messages[data.TargetID]

	links := b.findLinks(m.Content)
	if len(links) == 0 {
		respondEphemeral(s, i, "I found no song links in this message.")
		return
	}
	if missing := missingPermissions(i.AppPermissions, settings.Auto); len(missing) > 0 {
		respondEphemeral(s, i, "I can't reply in this channel, I'm missing: "+strings.Join(missing, ", "))
		return
	}

	// Translating can take longer than the 3 seconds Discord waits for an
	// answer. The translations are posted as replies to the message, like
	// in Auto mode, and the ephemeral answer only says how it went.
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
	if err != nil {
		log.Printf("Answering interaction: %v", err)
		return
	}

	m.GuildID = i.GuildID
	reply := "Done."
	if sent, _ := b.translateAndReply(s, i.ChannelID, m.Reference(), links); sent == 0 {
		reply = "I couldn't find these songs on other platforms."
	}
	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &reply}); err != nil {
		log.Printf("Editing interaction answer: %v", err)
	}
}

func respondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         content,
			Flags:           discordgo.MessageFlagsEphemeral,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	})
	if err != nil {
		log.Printf("Answering interaction: %v", err)
	}
}

func optionMap(opts []*discordgo.ApplicationCommandInteractionDataOption) map[string]*discordgo.ApplicationCommandInteractionDataOption {
	m := make(map[string]*discordgo.ApplicationCommandInteractionDataOption, len(opts))
	for _, o := range opts {
		m[o.Name] = o
	}
	return m
}
