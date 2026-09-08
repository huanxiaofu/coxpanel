package generator

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

func GenerateClient(format string, proxies []Proxy, name string, definition json.RawMessage) ([]byte, error) {
	started := time.Now()
	var content []byte
	var err error
	switch format {
	case "mihomo", "":
		if definition == nil {
			content, err = GenerateMihomo(proxies, name)
		} else {
			content, err = GenerateMihomoWithTemplate(proxies, name, definition)
		}
	case "sing-box":
		content, err = GenerateSingBoxWithTemplate(proxies, name, definition)
	case "base64":
		if definition != nil {
			var template *templateDefinition
			template, err = parseTemplateDefinition(format, definition)
			if err != nil {
				return nil, err
			}
			proxies = applyTemplateDefaults(proxies, name, template)
		}
		var raw []byte
		raw, err = GenerateURIList(proxies)
		content = []byte(base64.StdEncoding.EncodeToString(raw))
	default:
		return nil, errors.New("format unsupported")
	}
	if len(content) > 5*1024*1024 || time.Since(started) > 2*time.Second {
		return nil, errors.New("render limit exceeded")
	}
	return content, err
}
