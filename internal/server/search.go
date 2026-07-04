package server

import (
	"strings"
	"unicode"

	pinyin "github.com/mozillazg/go-pinyin"
)

var pinyinArgs = func() pinyin.Args {
	args := pinyin.NewArgs()
	args.Heteronym = true
	return args
}()

func buildSearchText(parts ...string) string {
	tokens := make([]string, 0, len(parts)*4)
	for _, part := range parts {
		tokens = appendSearchTokens(tokens, part)
	}
	return strings.Join(dedupeStrings(tokens), " ")
}

func normalizeSearchQuery(query string) string {
	return strings.Join(appendSearchTokens(nil, query), " ")
}

func searchLikePattern(query string) string {
	normalized := compactSearchText(strings.ToLower(strings.TrimSpace(query)))
	if normalized == "" {
		normalized = normalizeSearchQuery(query)
	}
	if normalized == "" {
		return ""
	}
	return "%" + normalized + "%"
}

func appendSearchTokens(tokens []string, value string) []string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return tokens
	}
	compact := compactSearchText(value)
	if compact != "" {
		tokens = append(tokens, value, compact)
	}
	words := pinyin.Pinyin(value, pinyinArgs)
	if len(words) == 0 {
		return tokens
	}
	full := make([]string, 0, len(words))
	initials := make([]rune, 0, len(words))
	for _, variants := range words {
		if len(variants) == 0 {
			continue
		}
		syllable := strings.ToLower(variants[0])
		full = append(full, syllable)
		if r := []rune(syllable); len(r) > 0 {
			initials = append(initials, r[0])
		}
	}
	if len(full) > 0 {
		tokens = append(tokens, strings.Join(full, ""), strings.Join(full, " "))
	}
	for _, variant := range pinyinCombinations(words, 64) {
		tokens = append(tokens, strings.Join(variant, ""), strings.Join(variant, " "))
	}
	if len(initials) > 0 {
		tokens = append(tokens, string(initials))
	}
	return tokens
}

func pinyinCombinations(words [][]string, limit int) [][]string {
	if len(words) == 0 || limit <= 0 {
		return nil
	}
	result := [][]string{{}}
	for _, variants := range words {
		if len(variants) == 0 {
			continue
		}
		next := make([][]string, 0, len(result)*len(variants))
		for _, prefix := range result {
			for _, variant := range dedupeStrings(variants) {
				combined := append(append([]string{}, prefix...), strings.ToLower(variant))
				next = append(next, combined)
				if len(next) >= limit {
					return next
				}
			}
		}
		result = next
	}
	return result
}

func compactSearchText(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.Is(unicode.Han, r) {
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	return builder.String()
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
