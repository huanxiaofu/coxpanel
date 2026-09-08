package generator

import (
	"encoding/json"
	"fmt"
	"strings"
)

type ClientOverride struct {
	DisplayName *string        `json:"displayName"`
	SortOrder   *int           `json:"sortOrder"`
	Icon        *string        `json:"icon"`
	ProxyGroup  *string        `json:"proxyGroup"`
	Params      map[string]any `json:"params"`
}

func ResolveClientOverride(proxy Proxy, definition, defaults json.RawMessage, override ClientOverride, names ...string) (OverrideData, map[string]string) {
	result := OverrideData{DisplayName: proxy.Name, Params: map[string]any{}}
	sources := map[string]string{"displayName": "node", "sortOrder": "node", "icon": "node", "proxyGroup": "node"}
	protocol := "external"
	if proxy.Inbound != nil {
		protocol = proxy.Inbound.Protocol
	}
	var material map[string]any
	if proxy.Inbound != nil {
		_ = json.Unmarshal(proxy.Inbound.Config, &material)
	}
	for key, value := range material {
		if ValidateOverrideParams("mihomo", protocol, map[string]any{key: value}) == nil && key != "obfsPassword" {
			result.Params[key] = value
			sources["params."+key] = "node"
		}
	}
	if proxy.Node != nil && proxy.Inbound != nil {
		server := proxy.Node.PublicIP
		if server == "" {
			server = proxy.Node.EasyIP
		}
		result.Params["server"] = server
		result.Params["port"] = proxy.Inbound.ListenPort
		sources["params.server"] = "node"
		sources["params.port"] = "node"
	}
	apply := func(value ClientOverride, source string) {
		if value.DisplayName != nil {
			result.DisplayName = *value.DisplayName
			sources["displayName"] = source
		}
		if value.SortOrder != nil {
			result.SortOrder = *value.SortOrder
			sources["sortOrder"] = source
		}
		if value.Icon != nil {
			result.Icon = *value.Icon
			sources["icon"] = source
		}
		if value.ProxyGroup != nil {
			result.ProxyGroup = *value.ProxyGroup
			sources["proxyGroup"] = source
		}
		for key, item := range value.Params {
			if ValidateOverrideParams("mihomo", protocol, map[string]any{key: item}) == nil {
				result.Params[key] = item
				sources["params."+key] = source
			}
		}
	}
	var template templateDefinition
	if json.Unmarshal(definition, &template) == nil {
		params := map[string]any{}
		for key, value := range template.Defaults.Params {
			if value != "" {
				params[key] = value
			}
		}
		apply(ClientOverride{Params: params}, "template")
		if template.Defaults.DisplayNamePattern != "" {
			variables := map[string]string{}
			for key, value := range template.Variables {
				if value.Default != nil {
					variables[key] = fmt.Sprint(value.Default)
				}
			}
			name := ""
			if len(names) > 0 {
				name = names[0]
			}
			result.DisplayName = expandTemplatePattern(template.Defaults.DisplayNamePattern, &proxy, name, variables)
			sources["displayName"] = "template"
		}
	}
	var group ClientOverride
	if json.Unmarshal(defaults, &group) == nil {
		apply(group, "group")
	}
	apply(override, "override")
	return result, sources
}

func ValidateClientGroup(definition json.RawMessage, group string) error {
	if group == "" {
		return nil
	}
	var template templateDefinition
	if json.Unmarshal(definition, &template) != nil {
		return ErrInvalidOverride
	}
	for _, entry := range template.Groups {
		if entry.ID == strings.TrimPrefix(group, "group:") {
			return nil
		}
	}
	return ErrInvalidOverride
}
