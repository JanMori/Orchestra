package handler

import (
	"testing"

	"github.com/JanMori/Orchestra/server/internal/featureflags"
	"github.com/JanMori/Orchestra/server/pkg/featureflag"
)

func withComposioMCPAppsFlag(t *testing.T, h *Handler, enabled bool) {
	withFeatureFlag(t, h, featureflags.ComposioMCPApps, enabled)
}

func withFeatureFlag(t *testing.T, h *Handler, key string, enabled bool) {
	t.Helper()
	provider := featureflag.NewStaticProvider()
	provider.Set(key, featureflag.Rule{Default: enabled})
	flags := featureflag.NewService(provider)

	origHandlerFlags := h.FeatureFlags
	h.FeatureFlags = flags
	var origTaskFlags *featureflag.Service
	if h.TaskService != nil {
		origTaskFlags = h.TaskService.FeatureFlags
		h.TaskService.FeatureFlags = flags
	}
	t.Cleanup(func() {
		h.FeatureFlags = origHandlerFlags
		if h.TaskService != nil {
			h.TaskService.FeatureFlags = origTaskFlags
		}
	})
}
