package generator

import (
	"fmt"
	"strconv"
	"strings"

	"api-tester/internal/config"
	"api-tester/internal/model"
)

type PayloadBuilder struct {
	cfg *config.Config
}

func NewPayloadBuilder(cfg *config.Config) *PayloadBuilder { return &PayloadBuilder{cfg: cfg} }

func (b *PayloadBuilder) Build(ep model.Endpoint) []map[string]any {
	if ov, ok := b.cfg.Overrides.ByPath[ep.Path]; ok && len(ov) > 0 {
		return expandMap(ov)
	}

	payload := map[string][]any{}
	for k, v := range b.cfg.Overrides.Defaults {
		payload[k] = []any{v}
	}
	for _, p := range ep.Params {
		if len(p.Enum) > 0 {
			vals := make([]any, 0, len(p.Enum))
			for _, v := range p.Enum {
				vals = append(vals, v)
			}
			payload[p.Name] = vals
			continue
		}
		if vals, ok := b.cfg.Overrides.ByParameterName[p.Name]; ok && len(vals) > 0 {
			payload[p.Name] = vals
			continue
		}
		payload[p.Name] = []any{b.guessValue(ep, p)}
	}

	if len(payload) == 0 {
		return []map[string]any{{}}
	}
	return expandMap(payload)
}

func (b *PayloadBuilder) guessValue(ep model.Endpoint, p model.Parameter) any {
	name := strings.ToLower(p.Name)
	ptype := strings.ToLower(p.Type)
	def := p.Default
	if def != "" && !strings.EqualFold(def, "none") && !strings.EqualFold(def, "null") {
		return normalizeType(def, ptype)
	}
	switch {
	case name == "first_image":
		return "https://imgpublic.ycomesc.live/upload_01/upload/20250902/2025090214525538512.jpeg"
	case name == "end_image":
		return "https://pic.rmb.bdstatic.com/bjh/250622/beautify/20019de7ad4a6bae7da522da5c3f1555.jpeg?for=bg"
	case name == "source_path":
		return "https://imgpublic.ycomesc.live/upload_01/upload/20250902/2025090214525538512.jpeg"
	case name == "target_path":
		if strings.Contains(ep.Path, "video") {
			return "https://imgpublic.ycomesc.live/upload_01/upload/20250902/2025090214525538512.jpeg"
		}
		return "https://pic.rmb.bdstatic.com/bjh/250622/beautify/20019de7ad4a6bae7da522da5c3f1555.jpeg?for=bg"
	case strings.Contains(name, "image") || strings.Contains(name, "img"):
		return "https://imgpublic.ycomesc.live/upload_01/upload/20250902/2025090214525538512.jpeg"
	case strings.Contains(name, "video") && strings.Contains(name, "url"):
		return "https://imgpublic.ycomesc.live/upload_01/upload/20250902/2025090214525538512.jpeg"
	case name == "prompt" || strings.Contains(name, "positive_prompt"):
		return "a portrait with natural lighting"
	case strings.Contains(name, "negative_prompt"):
		return "low quality, blurry"
	case name == "scene_name":
		return "default_scene"
	case name == "output_format":
		if strings.Contains(ep.Path, "video") {
			return "video"
		}
		return "png"
	case name == "bid":
		return "898b0d60-da10-44e0-a7eb-46b3455878e5"
	case name == "fee":
		return "10"
	case name == "title":
		return "12312323123"
	case name == "notify_url":
		return "https://quiet-whale-88.webhook.cool"
	case name == "hash_key":
		return ""
	case name == "task_id":
		return ""
	case name == "app_id":
		return ""
	case name == "is_encrypt":
		return false
	case name == "batch_size":
		return 1
	case name == "width":
		return 512
	case name == "height":
		return 512
	case name == "limit":
		return 10
	case name == "page":
		return 1
	case name == "page_size":
		return 10
	case strings.Contains(name, "username"):
		return "demo_user"
	case strings.Contains(name, "password"):
		return "demo_password"
	case ptype == "bool":
		return false
	case strings.Contains(ptype, "int"):
		return 1
	case strings.Contains(ptype, "float"):
		return 1.0
	}
	return ""
}

func normalizeType(v string, ptype string) any {
	v = strings.TrimSpace(strings.Trim(v, `"'`))
	switch {
	case strings.Contains(ptype, "int"):
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	case strings.Contains(ptype, "float"):
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	case strings.Contains(ptype, "bool"):
		return v == "True" || strings.EqualFold(v, "true")
	}
	return v
}

func expandMap(in map[string][]any) []map[string]any {
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	out := []map[string]any{{}}
	for _, k := range keys {
		values := in[k]
		if len(values) == 0 {
			values = []any{""}
		}
		next := make([]map[string]any, 0, len(out)*len(values))
		for _, existing := range out {
			for _, v := range values {
				cp := make(map[string]any, len(existing)+1)
				for ek, ev := range existing {
					cp[ek] = ev
				}
				cp[k] = sanitize(v)
				next = append(next, cp)
			}
		}
		out = next
	}
	return out
}

func sanitize(v any) any {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return t
	}
}

func PayloadPreview(ep model.Endpoint, payload map[string]any) string {
	return fmt.Sprintf("%s %s %#v", ep.Method, ep.Path, payload)
}
