package generator

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"api-tester/internal/config"
	"api-tester/internal/model"
)

type PayloadBuilder struct {
	cfg *config.Config
}

func NewPayloadBuilder(cfg *config.Config) *PayloadBuilder {
	return &PayloadBuilder{cfg: cfg}
}

func (b *PayloadBuilder) Build(ep model.Endpoint) []map[string]any {
	// Prefer per-path overrides.
	if ov, ok := b.cfg.Overrides.ByPath[ep.Path]; ok && len(ov) > 0 {
		return expandMap(ov)
	}

	payload := map[string][]any{}
	for _, p := range ep.Params {
		if len(p.Enum) > 0 {
			values := make([]any, 0, len(p.Enum))
			for _, v := range p.Enum {
				values = append(values, v)
			}
			payload[p.Name] = values
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
	if def != "" && def != "None" && def != "null" {
		return normalizeType(def, ptype)
	}
	switch {
	case name == "source_path":
		return "https://example.com/source.jpg"
	case name == "target_path":
		if strings.Contains(ep.Path, "video") {
			return "https://example.com/target.mp4"
		}
		return "https://example.com/target.jpg"
	case strings.Contains(name, "img") && strings.Contains(name, "url"):
		return "https://example.com/input.jpg"
	case strings.Contains(name, "video") && strings.Contains(name, "url"):
		return "https://example.com/input.mp4"
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
		return "smoke-test-bid"
	case name == "fee":
		return "1"
	case name == "title":
		return "api-tester"
	case name == "notify_url":
		return "https://example.com/webhook"
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
		if _, err := url.ParseRequestURI(t); err == nil && strings.HasPrefix(t, "https://example.com/") {
			return t
		}
		return strings.TrimSpace(t)
	default:
		return t
	}
}

func PayloadPreview(ep model.Endpoint, payload map[string]any) string {
	return fmt.Sprintf("%s %s %#v", ep.Method, ep.Path, payload)
}
