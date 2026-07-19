package screening

import (
	"context"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
	"testing"
)

func TestDemoProviderMatchesNormalizedName(t *testing.T) {
	matches, err := (DemoProvider{}).Screen(context.Background(), "Viktor A. Petrov")
	if err != nil || len(matches) != 1 || matches[0].ListType != domain.ScreeningSanctions || matches[0].Score < 70 {
		t.Fatalf("matches=%+v err=%v", matches, err)
	}
}
