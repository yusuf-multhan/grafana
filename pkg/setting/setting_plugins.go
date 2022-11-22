package setting

import (
	"fmt"
	"strings"

	"gopkg.in/ini.v1"
)

// PluginSettings maps plugin id to map of key/value settings.
type PluginSettings map[string]map[string]string

func extractPluginSettings(sections []*ini.Section) PluginSettings {
	psMap := PluginSettings{}
	for _, section := range sections {
		sectionName := section.Name()
		if !strings.HasPrefix(sectionName, "plugin.") {
			continue
		}

		pluginID := strings.Replace(sectionName, "plugin.", "", 1)
		psMap[pluginID] = section.KeysHash()
	}

	return psMap
}

// PluginsCDNMode is the mode used to serve assets from the plugins CDN.
type PluginsCDNMode string

// Valid values for PluginsCDNMode

const (
	PluginsCDNModeRedirect     PluginsCDNMode = "redirect"
	PluginsCDNModeReverveProxy PluginsCDNMode = "reverse_proxy"
)

// IsValid returns true if the PluginsCDNMode is equal to an allowed value.
func (m PluginsCDNMode) IsValid() bool {
	return m == PluginsCDNModeRedirect || m == PluginsCDNModeReverveProxy
}

// defaultHGPluginsCDNBaseURL is the default value for the CDN base path
// TODO: remove/change this before deploying to HG
const defaultHGPluginsCDNBaseURL = "https://grafana-assets.grafana.net/plugin-cdn-test/plugin-cdn"

func (cfg *Cfg) readPluginSettings(iniFile *ini.File) error {
	pluginsSection := iniFile.Section("plugins")
	pluginsCDNSection := iniFile.Section("plugins_cdn")

	cfg.PluginsEnableAlpha = pluginsSection.Key("enable_alpha").MustBool(false)
	cfg.PluginsAppsSkipVerifyTLS = pluginsSection.Key("app_tls_skip_verify_insecure").MustBool(false)
	cfg.PluginSettings = extractPluginSettings(iniFile.Sections())

	pluginsAllowUnsigned := pluginsSection.Key("allow_loading_unsigned_plugins").MustString("")

	for _, plug := range strings.Split(pluginsAllowUnsigned, ",") {
		plug = strings.TrimSpace(plug)
		cfg.PluginsAllowUnsigned = append(cfg.PluginsAllowUnsigned, plug)
	}

	cfg.PluginCatalogURL = pluginsSection.Key("plugin_catalog_url").MustString("https://grafana.com/grafana/plugins/")
	cfg.PluginAdminEnabled = pluginsSection.Key("plugin_admin_enabled").MustBool(true)
	cfg.PluginAdminExternalManageEnabled = pluginsSection.Key("plugin_admin_external_manage_enabled").MustBool(false)
	catalogHiddenPlugins := pluginsSection.Key("plugin_catalog_hidden_plugins").MustString("")

	for _, plug := range strings.Split(catalogHiddenPlugins, ",") {
		plug = strings.TrimSpace(plug)
		cfg.PluginCatalogHiddenPlugins = append(cfg.PluginCatalogHiddenPlugins, plug)
	}

	// Plugins CDN settings
	cfg.PluginsCDNMode = PluginsCDNMode(pluginsCDNSection.Key("mode").MustString(string(PluginsCDNModeRedirect)))
	if !cfg.PluginsCDNMode.IsValid() {
		return fmt.Errorf("plugins cdn mode %q is not valid", cfg.PluginsCDNMode)
	}
	cfg.PluginsCDNBasePath = strings.TrimRight(pluginsCDNSection.Key("url").MustString(defaultHGPluginsCDNBaseURL), "/")
	
	return nil
}
