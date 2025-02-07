package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/grafana/alerting/alerting/notifier/channels"
	"github.com/prometheus/alertmanager/notify"
	"github.com/prometheus/alertmanager/template"
	"github.com/prometheus/alertmanager/types"
	"github.com/prometheus/common/model"
)

// Constants and models are set according to the official documentation https://discord.com/developers/docs/resources/webhook#execute-webhook-jsonform-params

type discordEmbedType string

const (
	discordRichEmbed discordEmbedType = "rich"

	discordMaxEmbeds     = 10
	discordMaxMessageLen = 2000
)

type discordMessage struct {
	Username  string             `json:"username,omitempty"`
	Content   string             `json:"content"`
	AvatarURL string             `json:"avatar_url,omitempty"`
	Embeds    []discordLinkEmbed `json:"embeds,omitempty"`
}

// discordLinkEmbed implements https://discord.com/developers/docs/resources/channel#embed-object
type discordLinkEmbed struct {
	Title string           `json:"title,omitempty"`
	Type  discordEmbedType `json:"type,omitempty"`
	URL   string           `json:"url,omitempty"`
	Color int64            `json:"color,omitempty"`

	Footer *discordFooter `json:"footer,omitempty"`

	Image *discordImage `json:"image,omitempty"`
}

// discordFooter implements https://discord.com/developers/docs/resources/channel#embed-object-embed-footer-structure
type discordFooter struct {
	Text    string `json:"text"`
	IconURL string `json:"icon_url,omitempty"`
}

// discordImage implements https://discord.com/developers/docs/resources/channel#embed-object-embed-footer-structure
type discordImage struct {
	URL string `json:"url"`
}

type DiscordNotifier struct {
	*channels.Base
	log        channels.Logger
	ns         channels.WebhookSender
	images     channels.ImageStore
	tmpl       *template.Template
	settings   *discordSettings
	appVersion string
}

type discordSettings struct {
	Title              string `json:"title,omitempty" yaml:"title,omitempty"`
	Message            string `json:"message,omitempty" yaml:"message,omitempty"`
	AvatarURL          string `json:"avatar_url,omitempty" yaml:"avatar_url,omitempty"`
	WebhookURL         string `json:"url,omitempty" yaml:"url,omitempty"`
	UseDiscordUsername bool   `json:"use_discord_username,omitempty" yaml:"use_discord_username,omitempty"`
}

func buildDiscordSettings(fc channels.FactoryConfig) (*discordSettings, error) {
	var settings discordSettings
	err := json.Unmarshal(fc.Config.Settings, &settings)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
	}
	if settings.WebhookURL == "" {
		return nil, errors.New("could not find webhook url property in settings")
	}
	if settings.Title == "" {
		settings.Title = channels.DefaultMessageTitleEmbed
	}
	if settings.Message == "" {
		settings.Message = channels.DefaultMessageEmbed
	}
	return &settings, nil
}

type discordAttachment struct {
	url       string
	reader    io.ReadCloser
	name      string
	alertName string
	state     model.AlertStatus
}

func DiscordFactory(fc channels.FactoryConfig) (channels.NotificationChannel, error) {
	dn, err := newDiscordNotifier(fc)
	if err != nil {
		return nil, receiverInitError{
			Reason: err.Error(),
			Cfg:    *fc.Config,
		}
	}
	return dn, nil
}

func newDiscordNotifier(fc channels.FactoryConfig) (*DiscordNotifier, error) {
	settings, err := buildDiscordSettings(fc)
	if err != nil {
		return nil, err
	}
	return &DiscordNotifier{
		Base:       channels.NewBase(fc.Config),
		log:        fc.Logger,
		ns:         fc.NotificationService,
		images:     fc.ImageStore,
		tmpl:       fc.Template,
		settings:   settings,
		appVersion: fc.GrafanaBuildVersion,
	}, nil
}

func (d DiscordNotifier) Notify(ctx context.Context, as ...*types.Alert) (bool, error) {
	alerts := types.Alerts(as...)

	var msg discordMessage

	if !d.settings.UseDiscordUsername {
		msg.Username = "Grafana"
	}

	var tmplErr error
	tmpl, _ := channels.TmplText(ctx, d.tmpl, as, d.log, &tmplErr)

	msg.Content = tmpl(d.settings.Message)
	if tmplErr != nil {
		d.log.Warn("failed to template Discord notification content", "error", tmplErr.Error())
		// Reset tmplErr for templating other fields.
		tmplErr = nil
	}
	truncatedMsg, truncated := channels.TruncateInRunes(msg.Content, discordMaxMessageLen)
	if truncated {
		key, err := notify.ExtractGroupKey(ctx)
		if err != nil {
			return false, err
		}
		d.log.Warn("Truncated content", "key", key, "max_runes", discordMaxMessageLen)
		msg.Content = truncatedMsg
	}

	if d.settings.AvatarURL != "" {
		msg.AvatarURL = tmpl(d.settings.AvatarURL)
		if tmplErr != nil {
			d.log.Warn("failed to template Discord Avatar URL", "error", tmplErr.Error(), "fallback", d.settings.AvatarURL)
			msg.AvatarURL = d.settings.AvatarURL
			tmplErr = nil
		}
	}

	footer := &discordFooter{
		Text:    "Grafana v" + d.appVersion,
		IconURL: "https://grafana.com/static/assets/img/fav32.png",
	}

	var linkEmbed discordLinkEmbed

	linkEmbed.Title = tmpl(d.settings.Title)
	if tmplErr != nil {
		d.log.Warn("failed to template Discord notification title", "error", tmplErr.Error())
		// Reset tmplErr for templating other fields.
		tmplErr = nil
	}
	linkEmbed.Footer = footer
	linkEmbed.Type = discordRichEmbed

	color, _ := strconv.ParseInt(strings.TrimLeft(getAlertStatusColor(alerts.Status()), "#"), 16, 0)
	linkEmbed.Color = color

	ruleURL := joinUrlPath(d.tmpl.ExternalURL.String(), "/alerting/list", d.log)
	linkEmbed.URL = ruleURL

	embeds := []discordLinkEmbed{linkEmbed}

	attachments := d.constructAttachments(ctx, as, discordMaxEmbeds-1)
	for _, a := range attachments {
		color, _ := strconv.ParseInt(strings.TrimLeft(getAlertStatusColor(alerts.Status()), "#"), 16, 0)
		embed := discordLinkEmbed{
			Image: &discordImage{
				URL: a.url,
			},
			Color: color,
			Title: a.alertName,
		}
		embeds = append(embeds, embed)
	}

	msg.Embeds = embeds

	if tmplErr != nil {
		d.log.Warn("failed to template Discord message", "error", tmplErr.Error())
		tmplErr = nil
	}

	u := tmpl(d.settings.WebhookURL)
	if tmplErr != nil {
		d.log.Warn("failed to template Discord URL", "error", tmplErr.Error(), "fallback", d.settings.WebhookURL)
		u = d.settings.WebhookURL
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return false, err
	}

	cmd, err := d.buildRequest(u, body, attachments)
	if err != nil {
		return false, err
	}

	if err := d.ns.SendWebhook(ctx, cmd); err != nil {
		d.log.Error("failed to send notification to Discord", "error", err)
		return false, err
	}
	return true, nil
}

func (d DiscordNotifier) SendResolved() bool {
	return !d.GetDisableResolveMessage()
}

func (d DiscordNotifier) constructAttachments(ctx context.Context, as []*types.Alert, embedQuota int) []discordAttachment {
	attachments := make([]discordAttachment, 0)

	_ = withStoredImages(ctx, d.log, d.images,
		func(index int, image channels.Image) error {
			if embedQuota < 1 {
				return channels.ErrImagesDone
			}

			if len(image.URL) > 0 {
				attachments = append(attachments, discordAttachment{
					url:       image.URL,
					state:     as[index].Status(),
					alertName: as[index].Name(),
				})
				embedQuota--
				return nil
			}

			// If we have a local file, but no public URL, upload the image as an attachment.
			if len(image.Path) > 0 {
				base := filepath.Base(image.Path)
				url := fmt.Sprintf("attachment://%s", base)
				reader, err := openImage(image.Path)
				if err != nil && !errors.Is(err, channels.ErrImageNotFound) {
					d.log.Warn("failed to retrieve image data from store", "error", err)
					return nil
				}

				attachments = append(attachments, discordAttachment{
					url:       url,
					name:      base,
					reader:    reader,
					state:     as[index].Status(),
					alertName: as[index].Name(),
				})
				embedQuota--
			}
			return nil
		},
		as...,
	)

	return attachments
}

func (d DiscordNotifier) buildRequest(url string, body []byte, attachments []discordAttachment) (*channels.SendWebhookSettings, error) {
	cmd := &channels.SendWebhookSettings{
		URL:        url,
		HTTPMethod: "POST",
	}
	if len(attachments) == 0 {
		cmd.ContentType = "application/json"
		cmd.Body = string(body)
		return cmd, nil
	}

	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	defer func() {
		if err := w.Close(); err != nil {
			// Shouldn't matter since we already close w explicitly on the non-error path
			d.log.Warn("failed to close multipart writer", "error", err)
		}
	}()

	payload, err := w.CreateFormField("payload_json")
	if err != nil {
		return nil, err
	}

	if _, err := payload.Write(body); err != nil {
		return nil, err
	}

	for _, a := range attachments {
		if a.reader != nil { // We have an image to upload.
			err = func() error {
				defer func() { _ = a.reader.Close() }()
				part, err := w.CreateFormFile("", a.name)
				if err != nil {
					return err
				}
				_, err = io.Copy(part, a.reader)
				return err
			}()
			if err != nil {
				return nil, err
			}
		}
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	cmd.ContentType = w.FormDataContentType()
	cmd.Body = b.String()
	return cmd, nil
}
