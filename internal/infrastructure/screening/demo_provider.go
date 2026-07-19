package screening

import (
	"context"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
	"strings"
	"unicode"
)

type entry struct {
	name string
	kind domain.ScreeningListType
}
type DemoProvider struct{}

func (DemoProvider) Name() string { return "fccp-demo-screening-v1" }

var demoEntries = []entry{{"Viktor Petrov", domain.ScreeningSanctions}, {"Nadia Karim", domain.ScreeningPEP}, {"Orion Trading Company", domain.ScreeningSanctions}, {"Marcus Vale", domain.ScreeningAdverseMedia}}

func (DemoProvider) Screen(_ context.Context, name string) ([]application.ScreeningCandidate, error) {
	result := []application.ScreeningCandidate{}
	for _, item := range demoEntries {
		score := similarity(name, item.name)
		if score >= 70 {
			result = append(result, application.ScreeningCandidate{ListType: item.kind, Name: item.name, Score: score, Reason: "Normalized name similarity meets the 70% review threshold"})
		}
	}
	return result, nil
}
func normalize(value string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, value)), " ")
}
func similarity(a, b string) int {
	a, b = normalize(a), normalize(b)
	if a == b {
		return 100
	}
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return 90
	}
	aa, bb := strings.Fields(a), strings.Fields(b)
	matches := 0
	for _, x := range aa {
		for _, y := range bb {
			if x == y {
				matches++
				break
			}
		}
	}
	denominator := len(aa) + len(bb)
	if denominator == 0 {
		return 0
	}
	return 2 * matches * 100 / denominator
}
