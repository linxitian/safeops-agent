package context

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var arabicOrdinal = regexp.MustCompile(`第\s*([0-9]{1,3})\s*(?:个|项|条|份)?`)

func ResolveResource(reference string, resources []string) (string, int, error) {
	if len(resources) == 0 {
		return "", -1, errors.New("session has no selected resources")
	}
	normalized := strings.TrimSpace(reference)
	index := -1
	if match := arabicOrdinal.FindStringSubmatch(normalized); len(match) == 2 {
		value, _ := strconv.Atoi(match[1])
		index = value - 1
	} else {
		for text, value := range map[string]int{"第一个": 0, "第一项": 0, "首个": 0, "第二个": 1, "第二项": 1, "第三个": 2, "第三项": 2, "第四个": 3, "第四项": 3, "第五个": 4, "第五项": 4} {
			if strings.Contains(normalized, text) {
				index = value
				break
			}
		}
	}
	if strings.Contains(normalized, "最后一个") || strings.Contains(normalized, "最后一项") {
		index = len(resources) - 1
	}
	if index == -1 && hasCurrentResourcePronoun(normalized) {
		index = len(resources) - 1
	}
	if index < 0 {
		return "", -1, errors.New("resource reference is ambiguous; use an explicit ordinal")
	}
	if index >= len(resources) {
		return "", -1, errors.New("resource ordinal is outside the selected list")
	}
	return resources[index], index, nil
}

func hasCurrentResourcePronoun(value string) bool {
	for _, text := range []string{"这个", "该文件", "该项", "该资源", "它"} {
		if strings.Contains(value, text) {
			return true
		}
	}
	return false
}
