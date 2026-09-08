package generator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

var (
	ErrTemplateInvalid        = errors.New("invalid template definition")
	ErrTemplateFieldForbidden = errors.New("template field forbidden")
	ErrOverrideFieldForbidden = errors.New("override field forbidden")
	ErrInvalidOverride        = errors.New("invalid override")
)

type FormatCapabilities struct {
	Format          string   `json:"format"`
	TemplateEnabled bool     `json:"templateEnabled"`
	OverrideFields  []string `json:"overrideFields"`
}

type templateDefinition struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Defaults      templateDefaults            `json:"defaults"`
	Variables     map[string]templateVariable `json:"variables"`
	Groups        []templateGroup             `json:"groups"`
	Rules         []templateRule              `json:"rules"`
	DNS           *templateDNS                `json:"dns"`
}

type templateDefaults struct {
	DisplayNamePattern string         `json:"displayNamePattern"`
	Params             map[string]any `json:"params"`
}

type templateVariable struct {
	Type    string `json:"type"`
	Default any    `json:"default"`
}

type templateGroup struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Members []string `json:"members"`
	ProbeID string   `json:"probeId"`
}

type templateRule struct {
	Type   string `json:"type"`
	Value  string `json:"value"`
	Target string `json:"target"`
}

type templateDNS struct {
	Enabled    bool   `json:"enabled"`
	ResolverID string `json:"resolverId"`
}

var templateNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,31}$`)
var templatePlaceholderPattern = regexp.MustCompile(`\$\{([^{}]+)\}`)
var hostPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,251}[A-Za-z0-9])?$`)

var formatCapabilities = map[string]FormatCapabilities{
	"mihomo": {
		Format:          "mihomo",
		TemplateEnabled: true,
		OverrideFields:  []string{"displayName", "sortOrder", "icon", "proxyGroup", "server", "port", "sni", "fingerprint", "flow", "obfs", "obfsPassword", "upMbps", "downMbps", "insecure"},
	},
	"sing-box": {
		Format:          "sing-box",
		TemplateEnabled: true,
		OverrideFields:  []string{"displayName", "sortOrder", "icon", "proxyGroup", "server", "port", "sni", "fingerprint", "flow", "obfs", "obfsPassword", "upMbps", "downMbps", "insecure"},
	},
	"base64": {
		Format:          "base64",
		TemplateEnabled: true,
		OverrideFields:  []string{"displayName", "sortOrder", "icon", "proxyGroup"},
	},
}

var allowedTemplateTopLevel = map[string]struct{}{
	"schemaVersion": {}, "defaults": {}, "variables": {}, "groups": {}, "rules": {}, "dns": {},
}

func SupportedFormats() []string {
	return []string{"mihomo", "sing-box", "base64"}
}

func Capabilities(format string) (FormatCapabilities, bool) {
	capability, ok := formatCapabilities[format]
	if !ok {
		return FormatCapabilities{}, false
	}
	capability.OverrideFields = append([]string(nil), capability.OverrideFields...)
	return capability, true
}

func AllowedOverrideFields(format, protocol string) []string {
	capability, ok := Capabilities(format)
	if !ok {
		return nil
	}
	if format == "base64" || protocol == "external" {
		return capability.OverrideFields[:4]
	}
	if protocol == "" {
		return capability.OverrideFields
	}
	allowed := make([]string, 0, len(capability.OverrideFields))
	for _, field := range capability.OverrideFields[:4] {
		allowed = append(allowed, field)
	}
	switch protocol {
	case "vless-reality", "vless":
		allowed = append(allowed, "server", "port", "sni", "fingerprint", "flow")
	case "shadowsocks", "ss":
		allowed = append(allowed, "server", "port")
	case "hysteria2", "hy2":
		allowed = append(allowed, "server", "port", "sni", "obfs", "obfsPassword", "upMbps", "downMbps", "insecure")
	}
	return allowed
}

func ValidateTemplateDefinition(format string, raw json.RawMessage) error {
	_, err := parseTemplateDefinition(format, raw)
	return err
}

func parseTemplateDefinition(format string, raw json.RawMessage) (*templateDefinition, error) {
	if _, ok := Capabilities(format); !ok {
		return nil, fmt.Errorf("%w: format %s", ErrTemplateInvalid, format)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("%w: definition is required", ErrTemplateInvalid)
	}
	if len(raw) > 256*1024 {
		return nil, fmt.Errorf("%w: definition exceeds 256 KiB", ErrTemplateInvalid)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil || top == nil {
		return nil, fmt.Errorf("%w: definition must be an object", ErrTemplateInvalid)
	}
	if templateJSONDepth(raw) > 16 {
		return nil, fmt.Errorf("%w: nesting exceeds 16", ErrTemplateInvalid)
	}
	for key := range top {
		if _, ok := allowedTemplateTopLevel[key]; !ok {
			return nil, fmt.Errorf("%w: %s", ErrTemplateFieldForbidden, key)
		}
	}
	var definition templateDefinition
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&definition); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTemplateInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("%w: definition must contain one JSON value", ErrTemplateInvalid)
	}
	if definition.SchemaVersion != 1 {
		return nil, fmt.Errorf("%w: schemaVersion", ErrTemplateInvalid)
	}
	if err := validateDefaults(format, definition.Defaults); err != nil {
		return nil, err
	}
	if err := validateVariables(definition.Variables); err != nil {
		return nil, err
	}
	if err := validatePattern(definition.Defaults.DisplayNamePattern, definition.Variables); err != nil {
		return nil, err
	}
	if err := validateGroups(definition.Groups); err != nil {
		return nil, err
	}
	if err := validateRules(definition.Rules, definition.Groups); err != nil {
		return nil, err
	}
	if err := validateDNS(definition.DNS); err != nil {
		return nil, err
	}
	if format == "base64" && (len(definition.Groups) != 0 || len(definition.Rules) != 0 || definition.DNS != nil || len(definition.Defaults.Params) != 0) {
		return nil, fmt.Errorf("%w: base64 templates only support display names", ErrTemplateFieldForbidden)
	}
	return &definition, nil
}

func validateDefaults(format string, defaults templateDefaults) error {
	keys := map[string]struct{}{"displayNamePattern": {}, "params": {}}
	if defaults.DisplayNamePattern != "" && (len([]byte(defaults.DisplayNamePattern)) > 128 || strings.ContainsAny(defaults.DisplayNamePattern, "\r\n\x00")) {
		return fmt.Errorf("%w: displayNamePattern", ErrTemplateInvalid)
	}
	for key := range defaults.Params {
		if _, ok := keys[key]; ok {
			continue
		}
		if err := validateTemplateParam(format, "", key, defaults.Params[key], true); err != nil {
			return err
		}
	}
	if len(defaults.Params) > 0 {
		for key, value := range defaults.Params {
			if err := validateTemplateParam(format, "", key, value, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateVariables(variables map[string]templateVariable) error {
	for name, variable := range variables {
		if !templateNamePattern.MatchString(name) {
			return fmt.Errorf("%w: variable %s", ErrTemplateInvalid, name)
		}
		switch variable.Type {
		case "string":
			if variable.Default != nil {
				if _, ok := variable.Default.(string); !ok {
					return fmt.Errorf("%w: variable %s default", ErrTemplateInvalid, name)
				}
			}
		case "bool":
			if variable.Default != nil {
				if _, ok := variable.Default.(bool); !ok {
					return fmt.Errorf("%w: variable %s default", ErrTemplateInvalid, name)
				}
			}
		case "int":
			if variable.Default != nil {
				if number, ok := variable.Default.(float64); !ok || number != float64(int(number)) {
					return fmt.Errorf("%w: variable %s default", ErrTemplateInvalid, name)
				}
			}
		default:
			return fmt.Errorf("%w: variable %s type", ErrTemplateInvalid, name)
		}
	}
	return nil
}

func validatePattern(pattern string, variables map[string]templateVariable) error {
	for _, match := range templatePlaceholderPattern.FindAllStringSubmatch(pattern, -1) {
		name := match[1]
		switch {
		case name == "node.name", name == "inbound.name", name == "subscription.name":
		case strings.HasPrefix(name, "var."):
			if _, ok := variables[strings.TrimPrefix(name, "var.")]; !ok {
				return fmt.Errorf("%w: unknown variable %s", ErrTemplateInvalid, name)
			}
		default:
			return fmt.Errorf("%w: placeholder %s", ErrTemplateFieldForbidden, name)
		}
	}
	return nil
}

func validateGroups(groups []templateGroup) error {
	if len(groups) > 32 {
		return fmt.Errorf("%w: groups", ErrTemplateInvalid)
	}
	byID := make(map[string]templateGroup, len(groups))
	byName := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		if group.Name == "DIRECT" || group.Name == "REJECT" || group.Name == "direct" {
			return fmt.Errorf("%w: reserved group name", ErrTemplateInvalid)
		}
		if group.ID == "" || !templateNamePattern.MatchString(group.ID) || group.Name == "" || len([]byte(group.Name)) > 128 || strings.ContainsAny(group.Name, "\r\n\x00") {
			return fmt.Errorf("%w: group identity", ErrTemplateInvalid)
		}
		if _, exists := byID[group.ID]; exists {
			return fmt.Errorf("%w: duplicate group %s", ErrTemplateInvalid, group.ID)
		}
		if _, exists := byName[group.Name]; exists {
			return fmt.Errorf("%w: duplicate group name", ErrTemplateInvalid)
		}
		if group.Type != "select" && group.Type != "urltest" {
			return fmt.Errorf("%w: group type", ErrTemplateInvalid)
		}
		if group.Type == "urltest" && !allowedProbe(group.ProbeID) {
			return fmt.Errorf("%w: probeId", ErrTemplateFieldForbidden)
		}
		if len(group.Members) == 0 || len(group.Members) > 256 {
			return fmt.Errorf("%w: group members", ErrTemplateInvalid)
		}
		byID[group.ID] = group
		byName[group.Name] = struct{}{}
	}
	for _, group := range groups {
		for _, member := range group.Members {
			if member == "$authorizedProxies" || member == "DIRECT" {
				continue
			}
			if strings.HasPrefix(member, "group:") {
				if _, ok := byID[strings.TrimPrefix(member, "group:")]; !ok {
					return fmt.Errorf("%w: group reference %s", ErrTemplateInvalid, member)
				}
				continue
			}
			return fmt.Errorf("%w: member %s", ErrTemplateFieldForbidden, member)
		}
	}
	state := make(map[string]uint8, len(groups))
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return fmt.Errorf("%w: group cycle", ErrTemplateInvalid)
		}
		if state[id] == 2 {
			return nil
		}
		state[id] = 1
		for _, member := range byID[id].Members {
			if strings.HasPrefix(member, "group:") {
				if err := visit(strings.TrimPrefix(member, "group:")); err != nil {
					return err
				}
			}
		}
		state[id] = 2
		return nil
	}
	for id := range byID {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func validateRules(rules []templateRule, groups []templateGroup) error {
	if len(rules) > 1000 {
		return fmt.Errorf("%w: rules", ErrTemplateInvalid)
	}
	groupIDs := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		groupIDs[group.ID] = struct{}{}
	}
	matchCount := 0
	for index, rule := range rules {
		switch rule.Type {
		case "DOMAIN", "DOMAIN-SUFFIX", "IP-CIDR":
			if rule.Value == "" || strings.ContainsAny(rule.Value, ",\r\n\x00") {
				return fmt.Errorf("%w: rule value", ErrTemplateInvalid)
			}
			if rule.Type == "IP-CIDR" {
				if _, _, err := net.ParseCIDR(rule.Value); err != nil {
					return fmt.Errorf("%w: CIDR", ErrTemplateInvalid)
				}
			} else if !validServer(rule.Value) {
				return fmt.Errorf("%w: domain", ErrTemplateInvalid)
			}
		case "MATCH":
			if rule.Value != "" || index != len(rules)-1 {
				return fmt.Errorf("%w: MATCH must be last", ErrTemplateInvalid)
			}
			matchCount++
		default:
			return fmt.Errorf("%w: rule type", ErrTemplateFieldForbidden)
		}
		if !validRuleTarget(rule.Target, groupIDs) {
			return fmt.Errorf("%w: rule target", ErrTemplateInvalid)
		}
	}
	if matchCount > 1 {
		return fmt.Errorf("%w: multiple MATCH rules", ErrTemplateInvalid)
	}
	return nil
}

func validateDNS(dns *templateDNS) error {
	if dns == nil {
		return nil
	}
	if dns.ResolverID != "" && !allowedResolver(dns.ResolverID) {
		return fmt.Errorf("%w: resolverId", ErrTemplateFieldForbidden)
	}
	return nil
}

func validateTemplateParam(format, protocol, key string, value any, template bool) error {
	if template && key == "sni" && value == "" {
		return nil
	}
	if template && key == "obfsPassword" {
		return fmt.Errorf("%w: obfsPassword", ErrTemplateFieldForbidden)
	}
	if format == "base64" {
		return fmt.Errorf("%w: %s", ErrTemplateFieldForbidden, key)
	}
	if template && key == "insecure" {
		if insecure, ok := value.(bool); ok && insecure {
			return fmt.Errorf("%w: insecure", ErrTemplateFieldForbidden)
		}
		return fmt.Errorf("%w: insecure", ErrTemplateFieldForbidden)
	}
	if err := ValidateOverrideParams(format, protocol, map[string]any{key: value}); err != nil {
		if errors.Is(err, ErrOverrideFieldForbidden) || errors.Is(err, ErrInvalidOverride) {
			return fmt.Errorf("%w: %s", ErrTemplateFieldForbidden, key)
		}
		return err
	}
	return nil
}

func ValidateOverrideMetadata(displayName *string, sortOrder *int, icon, proxyGroup *string) error {
	for name, value := range map[string]*string{"displayName": displayName, "icon": icon, "proxyGroup": proxyGroup} {
		if value == nil {
			continue
		}
		if len([]byte(*value)) > 128 || strings.ContainsAny(*value, "\r\n\x00") {
			return fmt.Errorf("%w: %s", ErrInvalidOverride, name)
		}
	}
	if sortOrder != nil && (*sortOrder < -1000000 || *sortOrder > 1000000) {
		return fmt.Errorf("%w: sortOrder", ErrInvalidOverride)
	}
	if icon != nil && *icon != "" {
		if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`).MatchString(*icon) {
			return fmt.Errorf("%w: icon", ErrInvalidOverride)
		}
	}
	return nil
}

func ValidateOverrideParams(format, protocol string, values map[string]any) error {
	if _, ok := Capabilities(format); !ok {
		return fmt.Errorf("%w: format %s", ErrInvalidOverride, format)
	}
	allowed := make(map[string]struct{})
	for _, field := range AllowedOverrideFields(format, protocol) {
		allowed[field] = struct{}{}
	}
	for key, value := range values {
		if _, ok := allowed[key]; !ok || key == "displayName" || key == "sortOrder" || key == "icon" || key == "proxyGroup" {
			return fmt.Errorf("%w: %s", ErrOverrideFieldForbidden, key)
		}
		if value == nil {
			return fmt.Errorf("%w: %s", ErrInvalidOverride, key)
		}
		switch key {
		case "server":
			text, ok := value.(string)
			if !ok || !validServer(text) {
				return fmt.Errorf("%w: server", ErrInvalidOverride)
			}
		case "fingerprint":
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("%w: fingerprint", ErrInvalidOverride)
			}
			switch text {
			case "chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized":
			default:
				return fmt.Errorf("%w: fingerprint", ErrOverrideFieldForbidden)
			}
		case "sni":
			text, ok := value.(string)
			if !ok || !validSafeText(text, 2048) {
				return fmt.Errorf("%w: %s", ErrInvalidOverride, key)
			}
		case "flow":
			text, ok := value.(string)
			if !ok || (text != "" && text != "xtls-rprx-vision") {
				return fmt.Errorf("%w: flow", ErrOverrideFieldForbidden)
			}
		case "obfs":
			text, ok := value.(string)
			if !ok || text != "salamander" {
				return fmt.Errorf("%w: obfs", ErrOverrideFieldForbidden)
			}
		case "obfsPassword":
			text, ok := value.(string)
			if !ok || !validSafeText(text, 2048) {
				return fmt.Errorf("%w: obfsPassword", ErrInvalidOverride)
			}
		case "port":
			if number, ok := strictInteger(value); !ok || number < 1 || number > 65535 {
				return fmt.Errorf("%w: port", ErrInvalidOverride)
			}
		case "upMbps", "downMbps":
			if number, ok := strictInteger(value); !ok || number < 0 || number > 1000000 {
				return fmt.Errorf("%w: %s", ErrInvalidOverride, key)
			}
		case "insecure":
			if _, ok := value.(bool); !ok || protocol == "vless-reality" || protocol == "vless" {
				return fmt.Errorf("%w: insecure", ErrOverrideFieldForbidden)
			}
		}
	}
	return nil
}

func ValidateOverride(format, protocol string, displayName *string, sortOrder *int, icon, proxyGroup *string, params map[string]any) error {
	if format == "base64" && proxyGroup != nil && *proxyGroup != "" {
		return ErrOverrideFieldForbidden
	}
	if err := ValidateOverrideMetadata(displayName, sortOrder, icon, proxyGroup); err != nil {
		return err
	}
	return ValidateOverrideParams(format, protocol, params)
}

func GenerateMihomoWithTemplate(proxies []Proxy, subName string, definition json.RawMessage) ([]byte, error) {
	template, err := parseTemplateDefinition("mihomo", definition)
	if err != nil {
		return nil, err
	}
	prepared := applyTemplateDefaults(proxies, subName, template)
	reserved := []string{"DIRECT", "REJECT", subName}
	for _, group := range template.Groups {
		reserved = append(reserved, group.Name)
	}
	items, err := renderMihomoItems(prepared, reserved...)
	if err != nil {
		return nil, err
	}
	groupNames := make(map[string]string, len(template.Groups))
	groups := make([]map[string]any, 0, len(template.Groups))
	for _, group := range template.Groups {
		groupNames[group.ID] = group.Name
	}
	for _, group := range template.Groups {
		members := make([]string, 0)
		for _, member := range group.Members {
			selected := items
			if member == "$authorizedProxies" {
				selected = nil
				for _, item := range items {
					if item.group == "" || strings.TrimPrefix(item.group, "group:") == group.ID {
						selected = append(selected, item)
					}
				}
			}
			members = appendMihomoMember(members, member, selected, groupNames)
		}
		if len(members) == 0 {
			members = []string{"DIRECT"}
		}
		entry := map[string]any{"name": group.Name, "type": mihomoGroupType(group.Type), "proxies": members}
		if group.Type == "urltest" {
			entry["url"] = probeURL(group.ProbeID)
			entry["interval"] = 300
		}
		groups = append(groups, entry)
	}
	if len(groups) == 0 {
		members := make([]string, 0, len(items))
		for _, item := range items {
			members = append(members, item.name)
		}
		if len(members) == 0 {
			members = []string{"DIRECT"}
		}
		groups = append(groups, map[string]any{"name": subName, "type": "select", "proxies": members})
	}
	rules := make([]string, 0, len(template.Rules))
	for _, rule := range template.Rules {
		target := rule.Target
		if strings.HasPrefix(target, "group:") {
			target = groupNames[strings.TrimPrefix(target, "group:")]
		}
		if rule.Type == "MATCH" {
			rules = append(rules, "MATCH,"+target)
		} else {
			rules = append(rules, rule.Type+","+rule.Value+","+target)
		}
	}
	if len(rules) == 0 {
		rules = []string{"MATCH," + groups[0]["name"].(string)}
	}
	document := map[string]any{"proxies": itemMaps(items), "proxy-groups": groups, "rules": rules}
	if template.DNS != nil {
		document["dns"] = map[string]any{"enable": template.DNS.Enabled, "nameserver": []string{probeResolver(template.DNS.ResolverID)}}
	}
	return marshalMihomoDocument(document)
}

func GenerateSingBox(proxies []Proxy, subName string) ([]byte, error) {
	return generateSingBox(proxies, subName, nil)
}

func GenerateSingBoxWithTemplate(proxies []Proxy, subName string, definition json.RawMessage) ([]byte, error) {
	return generateSingBox(proxies, subName, definition)
}

func generateSingBox(proxies []Proxy, subName string, definition json.RawMessage) ([]byte, error) {
	var template *templateDefinition
	var err error
	if definition != nil {
		template, err = parseTemplateDefinition("sing-box", definition)
		if err != nil {
			return nil, err
		}
		proxies = applyTemplateDefaults(proxies, subName, template)
	}
	reserved := []string{"direct", subName}
	if template != nil {
		for _, group := range template.Groups {
			reserved = append(reserved, group.Name)
		}
	}
	items, err := renderSingBoxItems(proxies, reserved...)
	if err != nil {
		return nil, err
	}
	outbounds := make([]map[string]any, 0, len(items)+4)
	for _, item := range items {
		outbounds = append(outbounds, item.config)
	}
	routeRules := make([]map[string]any, 0)
	groupNames := map[string]string{}
	if template != nil {
		for _, group := range template.Groups {
			groupNames[group.ID] = group.Name
		}
		for _, group := range template.Groups {
			members := make([]string, 0)
			for _, member := range group.Members {
				selected := items
				if member == "$authorizedProxies" {
					selected = nil
					for _, item := range items {
						if item.group == "" || strings.TrimPrefix(item.group, "group:") == group.ID {
							selected = append(selected, item)
						}
					}
				}
				members = appendSingBoxMember(members, member, selected, groupNames)
			}
			if len(members) == 0 {
				members = []string{"direct"}
			}
			entry := map[string]any{"type": "selector", "tag": group.Name, "outbounds": members}
			if group.Type == "urltest" {
				entry["type"] = "urltest"
				entry["url"] = probeURL(group.ProbeID)
				entry["interval"] = "5m"
			}
			outbounds = append(outbounds, entry)
		}
	}
	if len(templateGroups(template)) == 0 {
		members := make([]string, 0, len(items))
		for _, item := range items {
			members = append(members, item.name)
		}
		if len(members) == 0 {
			members = []string{"direct"}
		}
		outbounds = append(outbounds, map[string]any{"type": "selector", "tag": subName, "outbounds": members})
	}
	outbounds = append(outbounds, map[string]any{"type": "direct", "tag": "direct"})
	finalTag := subName
	if template != nil && len(template.Groups) > 0 {
		finalTag = template.Groups[0].Name
	}
	if template != nil {
		for _, rule := range template.Rules {
			target := rule.Target
			if strings.HasPrefix(target, "group:") {
				target = groupNames[strings.TrimPrefix(target, "group:")]
			}
			entry := map[string]any{"outbound": target}
			if target == "DIRECT" {
				entry["outbound"] = "direct"
			}
			if target == "REJECT" {
				delete(entry, "outbound")
				entry["action"] = "reject"
			} else {
				entry["action"] = "route"
			}
			switch rule.Type {
			case "DOMAIN":
				entry["domain"] = []string{rule.Value}
			case "DOMAIN-SUFFIX":
				entry["domain_suffix"] = []string{rule.Value}
			case "IP-CIDR":
				entry["ip_cidr"] = []string{rule.Value}
			}
			routeRules = append(routeRules, entry)
		}
	}
	document := map[string]any{
		"log":       map[string]any{"level": "warn"},
		"outbounds": outbounds,
		"route":     map[string]any{"final": finalTag, "rules": routeRules},
	}
	if template != nil && template.DNS != nil && template.DNS.Enabled {
		resolver := map[string]any{"type": "local", "tag": "p2-resolver"}
		if template.DNS.ResolverID == "cloudflare" || template.DNS.ResolverID == "google" {
			server := "1.1.1.1"
			if template.DNS.ResolverID == "google" {
				server = "8.8.8.8"
			}
			resolver = map[string]any{"type": "https", "tag": "p2-resolver", "server": server}
		}
		document["dns"] = map[string]any{"servers": []map[string]any{resolver}, "final": "p2-resolver"}
	}
	content, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal sing-box config: %w", err)
	}
	return content, nil
}

func GenerateSubscriptionPreview(format string, proxies []Proxy, subName string, definition json.RawMessage) ([]byte, error) {
	redacted := redactProxies(proxies)
	return GenerateClient(format, redacted, subName, definition)
}

type renderedMihomoItem struct {
	name  string
	group string
	data  map[string]any
}

type renderedSingBoxItem struct {
	name   string
	group  string
	config map[string]any
}

func renderMihomoItems(proxies []Proxy, reserved ...string) ([]renderedMihomoItem, error) {
	ordered := orderedProxies(proxies)
	items := make([]renderedMihomoItem, 0, len(ordered))
	seen := make(map[string]int)
	for _, name := range reserved {
		seen[name] = 1
	}
	for _, proxy := range ordered {
		name, data, include, err := renderMihomoProxy(proxy)
		if err != nil {
			return nil, err
		}
		if !include {
			continue
		}
		name = uniqueProxyName(name, proxy, seen)
		data["name"] = name
		group := ""
		if proxy.Override != nil {
			group = proxy.Override.ProxyGroup
		}
		items = append(items, renderedMihomoItem{name: name, data: data, group: group})
	}
	return items, nil
}

func renderSingBoxItems(proxies []Proxy, reserved ...string) ([]renderedSingBoxItem, error) {
	ordered := orderedProxies(proxies)
	items := make([]renderedSingBoxItem, 0, len(ordered))
	seen := make(map[string]int)
	for _, name := range reserved {
		seen[name] = 1
	}
	for _, proxy := range ordered {
		name := proxy.Name
		if proxy.Override != nil && proxy.Override.DisplayName != "" {
			name = proxy.Override.DisplayName
		}
		config, include, err := renderSingBoxProxy(proxy)
		if err != nil {
			return nil, err
		}
		if !include {
			continue
		}
		name = uniqueProxyName(name, proxy, seen)
		config["tag"] = name
		group := ""
		if proxy.Override != nil {
			group = proxy.Override.ProxyGroup
		}
		items = append(items, renderedSingBoxItem{name: name, config: config, group: group})
	}
	return items, nil
}

func renderSingBoxProxy(proxy Proxy) (map[string]any, bool, error) {
	if proxy.Node == nil {
		return nil, false, errors.New("proxy node is nil")
	}
	name := proxy.Name
	if proxy.Override != nil && proxy.Override.DisplayName != "" {
		name = proxy.Override.DisplayName
	}
	var params map[string]any
	var err error
	protocol := ""
	if proxy.Node.Type == "external" {
		params, err = inboundParams(proxy.Node.ExtParams)
		protocol = proxy.Node.ExtProtocol
		if protocol == "" {
			protocol = strParam(params, "protocol")
		}
	} else {
		if proxy.Inbound == nil || strings.TrimSpace(proxy.Credential) == "" {
			return nil, false, nil
		}
		params, err = inboundParams(proxy.Inbound.Config)
		protocol = proxy.Inbound.Protocol
	}
	if err != nil {
		return nil, false, err
	}
	params = mergeParams(params, proxy.Override)
	server := strParam(params, "server")
	port, hasPort := intParam(params, "port")
	if proxy.Node.Type != "external" {
		server = firstNonEmpty(server, proxy.Node.PublicIP, proxy.Node.EasyIP)
		port = proxy.Inbound.ListenPort
		if value, ok := intParam(params, "port"); ok {
			port = value
		}
		hasPort = true
	}
	if server == "" || !hasPort || port < 1 || port > 65535 {
		return nil, false, fmt.Errorf("invalid sing-box proxy server or port")
	}
	result := map[string]any{"tag": name, "server": server, "server_port": port}
	switch protocol {
	case "vless-reality", "vless":
		credential := proxy.Credential
		if proxy.Node.Type == "external" {
			credential = strParam(params, "uuid")
		}
		if credential == "" {
			return nil, false, nil
		}
		result["type"] = "vless"
		result["uuid"] = credential
		result["flow"] = strParamDef(params, "flow", "xtls-rprx-vision")
		result["tls"] = map[string]any{
			"enabled":     true,
			"server_name": strParam(params, "sni"),
			"utls":        map[string]any{"enabled": true, "fingerprint": strParamDef(params, "fingerprint", "chrome")},
			"reality": map[string]any{
				"enabled":    true,
				"public_key": strParam(params, "publicKey"),
				"short_id":   strParam(params, "shortId"),
			},
		}
	case "shadowsocks", "ss":
		credential := proxy.Credential
		if proxy.Node.Type == "external" {
			credential = strParam(params, "password")
		} else {
			credential, err = ss2022SubscriptionCredential(params, proxy.Credential)
			if err != nil {
				return nil, false, err
			}
		}
		if credential == "" {
			return nil, false, nil
		}
		result["type"] = "shadowsocks"
		result["method"] = strParamDef(params, "method", "2022-blake3-aes-128-gcm")
		result["password"] = credential
	case "hysteria2", "hy2":
		credential := proxy.Credential
		if proxy.Node.Type == "external" {
			credential = strParam(params, "password")
		}
		if credential == "" {
			return nil, false, nil
		}
		result["type"] = "hysteria2"
		result["password"] = credential
		tls := map[string]any{"enabled": true}
		if sni := strParam(params, "sni"); sni != "" {
			tls["server_name"] = sni
		}
		if insecure, ok := boolParam(params, "insecure"); ok {
			tls["insecure"] = insecure
		}
		result["tls"] = tls
		obfs, obfsPassword := hysteriaObfs(params)
		if obfs != "" {
			result["obfs"] = map[string]any{"type": obfs, "password": obfsPassword}
		}
		if up, ok := nonNegativeIntParam(params, "upMbps"); ok {
			result["up_mbps"] = up
		}
		if down, ok := nonNegativeIntParam(params, "downMbps"); ok {
			result["down_mbps"] = down
		}
	default:
		return nil, false, fmt.Errorf("unsupported sing-box protocol: %s", protocol)
	}
	return result, true, nil
}

func orderedProxies(proxies []Proxy) []Proxy {
	ordered := append([]Proxy(nil), proxies...)
	sort.SliceStable(ordered, func(left, right int) bool {
		leftOrder := proxySortOrder(ordered[left])
		rightOrder := proxySortOrder(ordered[right])
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		leftNode, leftInbound := proxyKey(ordered[left])
		rightNode, rightInbound := proxyKey(ordered[right])
		if leftNode != rightNode {
			return leftNode < rightNode
		}
		return leftInbound < rightInbound
	})
	return ordered
}

func proxyKey(proxy Proxy) (int64, int64) {
	if proxy.Node == nil {
		return 0, 0
	}
	if proxy.Inbound == nil {
		return proxy.Node.ID, 0
	}
	return proxy.Node.ID, proxy.Inbound.ID
}

func uniqueProxyName(name string, proxy Proxy, seen map[string]int) string {
	if len(name) > 128 {
		for len(name) > 104 {
			_, size := utf8.DecodeLastRuneInString(name)
			name = name[:len(name)-size]
		}
	}
	nodeID, inboundID := proxyKey(proxy)
	candidate := name
	for suffix := 0; seen[candidate] > 0; suffix++ {
		candidate = fmt.Sprintf("%s-%d-%d", name, nodeID, inboundID)
		if suffix > 0 {
			candidate += fmt.Sprintf("-%d", suffix)
		}
	}
	seen[candidate] = 1
	return candidate
}

func applyTemplateDefaults(proxies []Proxy, subName string, template *templateDefinition) []Proxy {
	prepared := append([]Proxy(nil), proxies...)
	variables := make(map[string]string, len(template.Variables))
	for name, variable := range template.Variables {
		if variable.Default == nil {
			continue
		}
		variables[name] = fmt.Sprint(variable.Default)
	}
	for index := range prepared {
		proxy := &prepared[index]
		if template.Defaults.DisplayNamePattern != "" && (proxy.Override == nil || proxy.Override.DisplayName == "") {
			proxy.Name = expandTemplatePattern(template.Defaults.DisplayNamePattern, proxy, subName, variables)
		}
		if len(template.Defaults.Params) == 0 {
			continue
		}
		params := make(map[string]any, len(template.Defaults.Params))
		for key, value := range template.Defaults.Params {
			if text, ok := value.(string); ok && text == "" {
				continue
			}
			protocol := "external"
			if proxy.Inbound != nil {
				protocol = proxy.Inbound.Protocol
			}
			if ValidateOverrideParams("mihomo", protocol, map[string]any{key: value}) == nil {
				params[key] = value
			}
		}
		if proxy.Override == nil {
			proxy.Override = &OverrideData{Params: params}
			continue
		}
		merged := make(map[string]any, len(params)+len(proxy.Override.Params))
		for key, value := range params {
			merged[key] = value
		}
		for key, value := range proxy.Override.Params {
			merged[key] = value
		}
		copyOverride := *proxy.Override
		copyOverride.Params = merged
		proxy.Override = &copyOverride
	}
	return prepared
}

func expandTemplatePattern(pattern string, proxy *Proxy, subName string, variables map[string]string) string {
	nodeName, inboundName := "", ""
	if proxy != nil && proxy.Node != nil {
		nodeName = proxy.Node.Name
	}
	if proxy != nil && proxy.Inbound != nil {
		inboundName = proxy.Inbound.Name
	}
	return templatePlaceholderPattern.ReplaceAllStringFunc(pattern, func(value string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}")
		switch name {
		case "node.name":
			return nodeName
		case "inbound.name":
			return inboundName
		case "subscription.name":
			return subName
		default:
			return variables[strings.TrimPrefix(name, "var.")]
		}
	})
}

func appendMihomoMember(members []string, member string, items []renderedMihomoItem, groupNames map[string]string) []string {
	if member == "$authorizedProxies" {
		for _, item := range items {
			members = append(members, item.name)
		}
		return members
	}
	if strings.HasPrefix(member, "group:") {
		return append(members, groupNames[strings.TrimPrefix(member, "group:")])
	}
	return append(members, member)
}

func appendSingBoxMember(members []string, member string, items []renderedSingBoxItem, groupNames map[string]string) []string {
	if member == "DIRECT" {
		return append(members, "direct")
	}
	if member == "REJECT" {
		return members
	}
	if member == "$authorizedProxies" {
		for _, item := range items {
			members = append(members, item.name)
		}
		return members
	}
	if strings.HasPrefix(member, "group:") {
		return append(members, groupNames[strings.TrimPrefix(member, "group:")])
	}
	return append(members, member)
}

func templateGroups(template *templateDefinition) []templateGroup {
	if template == nil {
		return nil
	}
	return template.Groups
}

func itemMaps(items []renderedMihomoItem) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, item.data)
	}
	return result
}

func marshalMihomoDocument(document map[string]any) ([]byte, error) {
	content, err := yaml.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("marshal mihomo YAML: %w", err)
	}
	var output bytes.Buffer
	output.WriteString("# Coxpanel 订阅\n")
	output.WriteString("# 更新订阅即同步，勿在本文件手动修改\n\n")
	output.Write(content)
	return output.Bytes(), nil
}

func mihomoGroupType(value string) string {
	if value == "urltest" {
		return "url-test"
	}
	return "select"
}

func allowedProbe(value string) bool {
	return value == "" || value == "default" || value == "cloudflare" || value == "google"
}

func allowedResolver(value string) bool {
	return value == "" || value == "system" || value == "cloudflare" || value == "google"
}

func probeURL(value string) string {
	switch value {
	case "google":
		return "https://www.google.com/generate_204"
	case "cloudflare":
		return "https://cp.cloudflare.com/generate_204"
	default:
		return "https://www.gstatic.com/generate_204"
	}
}

func probeResolver(value string) string {
	switch value {
	case "cloudflare":
		return "https://1.1.1.1/dns-query"
	case "google":
		return "https://dns.google/dns-query"
	default:
		return "system"
	}
}

func validRuleTarget(target string, groups map[string]struct{}) bool {
	if target == "DIRECT" || target == "REJECT" {
		return true
	}
	if strings.HasPrefix(target, "group:") {
		_, ok := groups[strings.TrimPrefix(target, "group:")]
		return ok
	}
	return false
}

func validSafeText(value string, max int) bool {
	return value != "" && len([]byte(value)) <= max && !strings.ContainsAny(value, "\r\n\x00")
}

func validServer(value string) bool {
	if !validSafeText(value, 253) || strings.ContainsAny(value, "/\\?#@ ") {
		return false
	}
	if net.ParseIP(value) != nil {
		return true
	}
	return hostPattern.MatchString(value) && !strings.HasPrefix(value, ".") && !strings.HasSuffix(value, ".")
}

func strictInteger(value any) (int, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := strconv.Atoi(string(typed))
		return parsed, err == nil
	case float64:
		converted := int(typed)
		return converted, typed == float64(converted)
	case int:
		return typed, true
	case int64:
		converted := int(typed)
		return converted, int64(converted) == typed
	default:
		return 0, false
	}
}

func redactProxies(proxies []Proxy) []Proxy {
	redacted := append([]Proxy(nil), proxies...)
	for index := range redacted {
		proxy := &redacted[index]
		proxy.Credential = "<credential>"
		if proxy.Override != nil {
			copyOverride := *proxy.Override
			copyOverride.Params = map[string]any{}
			for key, value := range proxy.Override.Params {
				if key == "obfsPassword" {
					copyOverride.Params[key] = "<configured>"
				} else {
					copyOverride.Params[key] = value
				}
			}
			proxy.Override = &copyOverride
		}
		if proxy.Node != nil {
			nodeCopy := *proxy.Node
			nodeCopy.ExtParams = redactRawParams(nodeCopy.ExtParams)
			proxy.Node = &nodeCopy
		}
		if proxy.Inbound != nil {
			inboundCopy := *proxy.Inbound
			inboundCopy.Config = redactRawParams(inboundCopy.Config)
			proxy.Inbound = &inboundCopy
		}
	}
	return redacted
}

func redactRawParams(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var values map[string]any
	if json.Unmarshal(raw, &values) != nil {
		return json.RawMessage(`{"configured":true}`)
	}
	for key := range values {
		switch key {
		case "password", "uuid", "privateKey", "private_key", "publicKey", "public_key", "shortId", "short_id", "certificatePath", "certificate_path", "keyPath", "key_path", "obfsPassword":
			values[key] = "<configured>"
		case "obfs":
			if nested, ok := values[key].(map[string]any); ok {
				if _, exists := nested["password"]; exists {
					nested["password"] = "<configured>"
				}
			}
		}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return json.RawMessage(`{"configured":true}`)
	}
	return encoded
}

func templateJSONDepth(raw []byte) int {
	depth, maximum := 0, 0
	inString, escaped := false, false
	for _, character := range raw {
		if inString {
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == '"' {
				inString = false
			}
			continue
		}
		switch character {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > maximum {
				maximum = depth
			}
		case '}', ']':
			if depth > 0 {
				depth--
			}
		}
	}
	return maximum
}
