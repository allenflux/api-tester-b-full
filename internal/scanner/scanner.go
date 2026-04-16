package scanner

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"api-tester/internal/config"
	"api-tester/internal/model"
)

var (
	routeStartRE = regexp.MustCompile(`@router\.(get|post|put|delete|patch)\(`)
	funcRE       = regexp.MustCompile(`async\s+def\s+([a-zA-Z0-9_]+)\s*\(`)
	tagRE        = regexp.MustCompile(`tags=\[(.*?)\]`)
	pathRE       = regexp.MustCompile(`["']([^"']+)["']`)
	paramRE      = regexp.MustCompile(`(?m)^\s*([a-zA-Z_][a-zA-Z0-9_]*)(?:\s*:\s*([^=,\n]+))?\s*=\s*(Form|Query|Body|File)\((.*?)\)\s*,?\s*$`)
	enumRE       = regexp.MustCompile(`enum\s*=\s*\[([^\]]+)\]`)
	descRE       = regexp.MustCompile(`description\s*=\s*["']([^"']*)["']`)
	defaultRE    = regexp.MustCompile(`default\s*=\s*([^,\)]+)`)
)

type Scanner struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Scanner {
	return &Scanner{cfg: cfg}
}

func (s *Scanner) Scan() ([]model.Endpoint, error) {
	var out []model.Endpoint
	for _, dir := range s.cfg.Source.RouterDirs {
		root := filepath.Join(s.cfg.Source.ProjectRoot, dir)
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				for _, ex := range s.cfg.Source.ExcludePaths {
					if strings.Contains(path, ex) {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if filepath.Ext(path) != ".py" {
				return nil
			}
			endpoints, err := s.scanFile(path)
			if err != nil {
				return fmt.Errorf("scan file %s: %w", path, err)
			}
			out = append(out, endpoints...)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Scanner) scanFile(path string) ([]model.Endpoint, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(raw)
	lines := strings.Split(text, "\n")
	var endpoints []model.Endpoint

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if !routeStartRE.MatchString(line) {
			continue
		}
		method := strings.ToUpper(routeStartRE.FindStringSubmatch(line)[1])

		block := []string{line}
		paren := strings.Count(line, "(") - strings.Count(line, ")")
		j := i + 1
		for ; j < len(lines) && paren > 0; j++ {
			block = append(block, lines[j])
			paren += strings.Count(lines[j], "(") - strings.Count(lines[j], ")")
		}
		routeBlock := strings.Join(block, "\n")
		epPath := firstStringLiteral(routeBlock)
		if epPath == "" {
			continue
		}

		funcStart := j
		for ; funcStart < len(lines); funcStart++ {
			if strings.Contains(lines[funcStart], "def ") {
				break
			}
		}
		if funcStart >= len(lines) {
			continue
		}
		funcBlockLines := []string{lines[funcStart]}
		paren = strings.Count(lines[funcStart], "(") - strings.Count(lines[funcStart], ")")
		k := funcStart + 1
		for ; k < len(lines) && paren > 0; k++ {
			funcBlockLines = append(funcBlockLines, lines[k])
			paren += strings.Count(lines[k], "(") - strings.Count(lines[k], ")")
		}
		funcBlock := strings.Join(funcBlockLines, "\n")
		fn := ""
		if m := funcRE.FindStringSubmatch(funcBlock); len(m) > 1 {
			fn = m[1]
		}
		tags := extractTags(routeBlock)
		params := extractParams(funcBlock)
		hasTaskID := false
		for _, p := range params {
			if p.Name == "task_id" || p.Name == "hash_key" || p.Name == "notify_url" {
				hasTaskID = true
			}
		}
		if forced, ok := s.cfg.Overrides.ForceMethods[epPath]; ok && forced != "" {
			method = strings.ToUpper(forced)
		}
		endpoints = append(endpoints, model.Endpoint{
			Method:        method,
			Path:          epPath,
			FuncName:      fn,
			SourceFile:    path,
			Tags:          tags,
			Params:        params,
			HasTaskID:     hasTaskID || strings.Contains(epPath, "/generate/") || strings.Contains(epPath, "/dispatcher/"),
			Active:        !contains(s.cfg.Overrides.DisabledPaths, epPath),
			DiscoveryHash: hashFor(path + routeBlock + funcBlock),
		})
		i = k - 1
	}
	return endpoints, nil
}

func firstStringLiteral(s string) string {
	if m := pathRE.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}

func extractTags(s string) []string {
	m := tagRE.FindStringSubmatch(s)
	if len(m) < 2 {
		return nil
	}
	items := strings.Split(m[1], ",")
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(strings.Trim(item, `"'`))
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func extractParams(funcBlock string) []model.Parameter {
	matches := paramRE.FindAllStringSubmatch(funcBlock, -1)
	var out []model.Parameter
	for _, m := range matches {
		p := model.Parameter{
			Name:     m[1],
			Type:     cleanType(m[2]),
			In:       strings.ToLower(m[3]),
			Required: strings.Contains(m[4], "..."),
		}
		inside := m[4]
		if dm := defaultRE.FindStringSubmatch(inside); len(dm) > 1 {
			p.Default = strings.TrimSpace(strings.Trim(dm[1], `"'`))
		}
		if desc := descRE.FindStringSubmatch(inside); len(desc) > 1 {
			p.Description = desc[1]
		}
		if em := enumRE.FindStringSubmatch(inside); len(em) > 1 {
			enumItems := strings.Split(em[1], ",")
			for _, item := range enumItems {
				item = strings.TrimSpace(strings.Trim(item, `"'`))
				if item != "" {
					p.Enum = append(p.Enum, item)
				}
			}
		}
		if p.Type == "" {
			p.Type = "string"
		}
		out = append(out, p)
	}
	return out
}

func cleanType(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "Annotated[")
	s = strings.TrimSuffix(s, "]")
	s = strings.TrimSpace(strings.Split(s, ",")[0])
	s = strings.TrimPrefix(s, "Optional[")
	s = strings.TrimSuffix(s, "]")
	return s
}

func hashFor(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
