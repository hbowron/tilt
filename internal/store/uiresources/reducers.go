package uiresources

import (
	"fmt"

	"github.com/tilt-dev/tilt/internal/store"
	"github.com/tilt-dev/tilt/pkg/logger"
	"github.com/tilt-dev/tilt/pkg/model"
	"github.com/tilt-dev/tilt/pkg/model/logstore"
)

func HandleUIResourceUpsertAction(state *store.EngineState, action UIResourceUpsertAction) {
	n := action.UIResource.Name
	old := state.UIResources[n]
	uir := action.UIResource
	if old != nil {
		oldCount := old.Status.DisableStatus.DisabledCount
		newCount := uir.Status.DisableStatus.DisabledCount

		verb := ""
		if oldCount == 0 && newCount > 0 {
			verb = "disabled"
		} else if oldCount > 0 && newCount == 0 {
			verb = "enabled"
		}

		if verb != "" {
			message := fmt.Sprintf("Resource %q %s. To enable/disable it, use the Tilt Web UI.\n", n, verb)
			a := store.NewLogAction(model.ManifestName(n), logstore.SpanID(fmt.Sprintf("disabletoggle-%s", n)), logger.InfoLvl, nil, []byte(message))
			state.LogStore.Append(a, state.Secrets)
		}

		ms, ok := state.ManifestState(model.ManifestName(n))

		if ok {
			if len(uir.Status.DisableStatus.Sources) > 0 {
				if uir.Status.DisableStatus.PendingCount > 0 {
					ms.EnabledStatus = store.EnabledStatusPending
				} else {
					if uir.Status.DisableStatus.DisabledCount > 0 {
						ms.EnabledStatus = store.EnabledStatusDisabled
					} else if uir.Status.DisableStatus.EnabledCount > 0 {
						ms.EnabledStatus = store.EnabledStatusEnabled
					}
				}
				if ms.EnabledStatus == store.EnabledStatusDisabled {
					// since file watches are disabled while a resource is disabled, we can't
					// have confidence in any previous build state
					ms.BuildHistory = nil
					if len(ms.BuildStatuses) > 0 {
						ms.BuildStatuses = make(map[model.TargetID]*store.BuildStatus)
					}
					state.RemoveFromTriggerQueue(ms.Name)
				}
			} else {
				// if a uiresource doesn't have any disablesources, it's always treated as enabled
				ms.EnabledStatus = store.EnabledStatusEnabled
			}
		}

	}

	state.UIResources[n] = uir
}

func HandleUIResourceDeleteAction(state *store.EngineState, action UIResourceDeleteAction) {
	delete(state.UIResources, action.Name)
}
