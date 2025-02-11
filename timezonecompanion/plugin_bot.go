package timezonecompanion

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/jonas747/dcmd"
	"github.com/jonas747/discordgo"
	"github.com/jonas747/yagpdb/bot"
	"github.com/jonas747/yagpdb/bot/eventsystem"
	"github.com/jonas747/yagpdb/bot/paginatedmessages"
	"github.com/jonas747/yagpdb/commands"
	"github.com/jonas747/yagpdb/common"
	"github.com/jonas747/yagpdb/timezonecompanion/models"
	"github.com/volatiletech/sqlboiler/boil"
	"math"
	"strings"
	"time"
)

var _ bot.BotInitHandler = (*Plugin)(nil)
var _ commands.CommandProvider = (*Plugin)(nil)

func (p *Plugin) BotInit() {
	eventsystem.AddHandlerAsyncLast(p.handleMessageCreate, eventsystem.EventMessageCreate)
}

func (p *Plugin) AddCommands() {
	commands.AddRootCommands(&commands.YAGCommand{
		CmdCategory:  commands.CategoryTool,
		Name:         "settimezone",
		Aliases:      []string{"setz", "tzset"},
		Description:  "Sets your timezone, used for various purposes such as auto conversion. Give it your country.",
		RequiredArgs: 1,
		Arguments: []*dcmd.ArgDef{
			&dcmd.ArgDef{Name: "Timezone", Type: dcmd.String},
		},
		RunFunc: func(parsed *dcmd.Data) (interface{}, error) {

			zones := FindZone(parsed.Args[0].Str())
			if len(zones) < 1 {
				return "Unknown timezone, enter a country or timezone (not abbreviation like CET). there's a timezone picker here: <http://kevalbhatt.github.io/timezone-picker> you can use, enter the `Area/City` result", nil
			}

			if len(zones) > 1 {
				if len(zones) > 10 {
					_, err := paginatedmessages.CreatePaginatedMessage(
						parsed.GS.ID, parsed.CS.ID, 1, int(math.Ceil(float64(len(zones))/10)), paginatedTimezones(zones))
					return nil, err
				}

				out := "More than 1 result, reuse the command with a one of the following:\n"
				for _, v := range zones {
					if s := StrZone(v); s != "" {
						out += s + "\n"
					}
				}

				return out, nil
			}
			loc, err := time.LoadLocation(zones[0])
			if err != nil {
				return "Unknown timezone", nil
			}

			name, _ := time.Now().In(loc).Zone()
			zone := zones[0]

			m := &models.UserTimezone{
				UserID:       parsed.Msg.Author.ID,
				TimezoneName: zone,
			}
			err = m.UpsertG(parsed.Context(), true, []string{"user_id"}, boil.Infer(), boil.Infer())
			if err != nil {
				return nil, err
			}

			return fmt.Sprintf("Set your timezone to `%s`: %s\n", zone, name), nil
		},
	}, &commands.YAGCommand{
		CmdCategory:         commands.CategoryTool,
		Name:                "ToggleTimeConversion",
		Aliases:             []string{"toggletconv"},
		Description:         "Toggles automatic time conversion for people with registered timezones (setz) in this channel, its on by default, toggle all channels by giving it `all`",
		RequireDiscordPerms: []int64{discordgo.PermissionManageMessages, discordgo.PermissionManageServer},
		Arguments: []*dcmd.ArgDef{
			&dcmd.ArgDef{Name: "falgs", Type: dcmd.String},
		},
		RunFunc: func(parsed *dcmd.Data) (interface{}, error) {
			allStr := parsed.Args[0].Str()
			all := false
			if strings.EqualFold(allStr, "all") || strings.EqualFold(allStr, "*") {
				all = true
			}

			insert := false
			conf, err := models.FindTimezoneGuildConfigG(parsed.Context(), parsed.GS.ID)
			if err != nil {
				if err == sql.ErrNoRows {
					conf = &models.TimezoneGuildConfig{
						GuildID: parsed.GS.ID,
					}
					insert = true
				} else {
					return nil, err
				}
			}

			resp := ""
			if all {
				if conf.NewChannelsDisabled {
					conf.NewChannelsDisabled = false
					conf.DisabledInChannels = []int64{}
					resp = "Enabled time conversion in all channels."
				} else {
					conf.NewChannelsDisabled = true
					conf.EnabledInChannels = []int64{}
					resp = "Disabled time conversion in all channels, including newly created channels."
				}
			} else {
				status := "off"

				found := false
				for i, v := range conf.DisabledInChannels {
					if v == parsed.CS.ID {
						found = true
						conf.DisabledInChannels = append(conf.DisabledInChannels[:i], conf.DisabledInChannels[i+1:]...)
						status = "on"

						if conf.NewChannelsDisabled {
							conf.EnabledInChannels = append(conf.EnabledInChannels, parsed.CS.ID)
						}

						break
					}
				}

				if !found {
					conf.DisabledInChannels = append(conf.DisabledInChannels, parsed.CS.ID)

					for i, v := range conf.EnabledInChannels {
						if v == parsed.CS.ID {
							conf.EnabledInChannels = append(conf.EnabledInChannels[:i], conf.EnabledInChannels[i+1:]...)
						}
					}
				}

				resp = fmt.Sprintf("Automatic time conversion in this channel toggled `%s`", status)
			}

			if insert {
				err = conf.InsertG(parsed.Context(), boil.Infer())
			} else {
				_, err = conf.UpdateG(parsed.Context(), boil.Infer())
			}

			if err != nil {
				return nil, err
			}

			return resp, nil
		},
	})
}

func StrZone(zone string) string {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return ""
	}

	name, _ := time.Now().In(loc).Zone()

	return fmt.Sprintf("`%s`: %s", zone, name)
}

func paginatedTimezones(timezones []string) func(p *paginatedmessages.PaginatedMessage, page int) (*discordgo.MessageEmbed, error) {
	return func(p *paginatedmessages.PaginatedMessage, page int) (*discordgo.MessageEmbed, error) {
		numSkip := (page - 1) * 10

		out := ""
		numAdded := 0
		for i := numSkip; i < len(timezones); i++ {
			if s := StrZone(timezones[i]); s != "" {
				out += s + "\n"
				numAdded++
				if numAdded >= 10 {
					break
				}
			}
		}

		return &discordgo.MessageEmbed{
			Description: "Please redo the command with one of the following:\n" + out,
		}, nil
	}
}

func GetUserTimezone(userID int64) *time.Location {
	m, err := models.FindUserTimezoneG(context.Background(), userID)
	if err != nil {
		return nil
	}

	loc, err := time.LoadLocation(m.TimezoneName)
	if err != nil {
		logger.WithError(err).Error("failed loading location")
		return nil
	}

	return loc
}

func FindZone(in string) []string {
	lowerIn := strings.ToLower(in)
	inSpaceReplaced := strings.ReplaceAll(lowerIn, " ", "_")

	ccs := make([]string, 0)
	for country, code := range CountryCodes {
		if strings.Contains(strings.ToLower(country), lowerIn) {
			ccs = append(ccs, code)
		}
	}

	matchesZones := make([]string, 0)

	for code, zones := range CCToZones {
		// if common.ContainsString()

		// check if we specified the country
		if common.ContainsStringSlice(ccs, code) || strings.EqualFold(code, lowerIn) {
			for _, v := range zones {
				matchesZones = append(matchesZones, v)
			}

			continue
		}

		for _, v := range zones {
			if strings.Contains(strings.ToLower(v), inSpaceReplaced) {
				matchesZones = append(matchesZones, v)
			}
		}
	}

	return matchesZones
}

func (p *Plugin) handleMessageCreate(evt *eventsystem.EventData) {
	m := evt.MessageCreate()
	if m.GuildID == 0 {
		return
	}

	result, err := p.DateParser.Parse(m.Content, time.Now())
	if err != nil || result == nil {
		return
	}

	conf, err := models.FindTimezoneGuildConfigG(evt.Context(), m.GuildID)
	if err != nil {
		if err != sql.ErrNoRows {
			logger.WithError(err).WithField("guild", m.GuildID).Error("failed fetching guild config")
			return
		}
	} else if common.ContainsInt64Slice(conf.DisabledInChannels, m.ChannelID) || (conf.NewChannelsDisabled && !common.ContainsInt64Slice(conf.EnabledInChannels, m.ChannelID)) {
		// disabled in this channel
		return
	}

	zone := GetUserTimezone(m.Author.ID)
	if zone == nil {
		return
	}

	// re-parse it with proper context
	result, err = p.DateParser.Parse(m.Content, time.Now().In(zone))
	if err != nil || result == nil {
		return
	}

	common.BotSession.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
		Timestamp: result.Time.Format(time.RFC3339),
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Above time (" + result.Time.Format("15:04 MST") + ") in your local time",
		},
	})

	// common.BotSession.ChannelMessageSend(m.ChannelID, "Time: `"+result.Time.Format(time.RFC822)+"`")
}
