package levant

import (
	"fmt"
	"strings"
	"sync"

	nomad "github.com/hashicorp/nomad/api"
	nomadStructs "github.com/hashicorp/nomad/nomad/structs"
	"github.com/jrasell/levant/logging"
)

// checkFailedDeployment helps log information about deployment failures.
func (l *levantDeployment) checkFailedDeployment(depID *string) {

	var allocIDS []string

	allocs, _, err := l.nomad.Deployments().Allocations(*depID, nil)
	if err != nil {
		logging.Error("levant/failure_inspector: unable to query deployment allocations for deployment %s",
			depID)
	}

	// Iterate the allocations on the deployment and create a list of each allocID
	// to inspect that is not running.
	for _, alloc := range allocs {
		if alloc.ClientStatus != nomadStructs.AllocClientStatusRunning {
			allocIDS = append(allocIDS, alloc.ID)
		}
	}

	// Setup a waitgroup so the function doesn't return until all allocations have
	// been inspected.
	var wg sync.WaitGroup
	wg.Add(+len(allocIDS))

	// Inspect each allocation.
	for _, id := range allocIDS {
		logging.Debug("levant/failure_inspector: launching allocation inspector for alloc %v", id)
		go l.allocInspector(id, &wg)
	}

	wg.Wait()
}

// allocInspector inspects an allocations events to log any useful information
// which may help debug deployment failures.
func (l *levantDeployment) allocInspector(allocID string, wg *sync.WaitGroup) {

	// Inform the wait group we have finished our task upon completion.
	defer wg.Done()

	resp, _, err := l.nomad.Allocations().Info(allocID, nil)
	if err != nil {
		logging.Error("levant/failure_inspector: unable to query alloc %v: %v", allocID, err)
		return
	}

	// Iterate each each Task and Event to log any relevant information which may
	// help debug deployment failures.
	for _, task := range resp.TaskStates {
		for _, event := range task.Events {

			var desc string

			switch event.Type {
			case nomad.TaskFailedValidation:
				if event.ValidationError != "" {
					desc = event.ValidationError
				} else {
					desc = "validation of task failed"
				}
			case nomad.TaskSetupFailure:
				if event.SetupError != "" {
					desc = event.SetupError
				} else {
					desc = "task setup failed"
				}
			case nomad.TaskDriverFailure:
				if event.DriverError != "" {
					desc = event.DriverError
				} else {
					desc = "failed to start task"
				}
			case nomad.TaskArtifactDownloadFailed:
				if event.DownloadError != "" {
					desc = event.DownloadError
				} else {
					desc = "the task failed to download artifacts"
				}
			case nomad.TaskKilling:
				if event.KillReason != "" {
					desc = fmt.Sprintf("the task was killed: %v", event.KillReason)
				} else if event.KillTimeout != 0 {
					desc = fmt.Sprintf("sent interrupt, waiting %v before force killing", event.KillTimeout)
				} else {
					desc = "the task was sent interrupt"
				}
			case nomad.TaskKilled:
				if event.KillError != "" {
					desc = event.KillError
				} else {
					desc = "the task was successfully killed"
				}
			case nomad.TaskTerminated:
				var parts []string
				parts = append(parts, fmt.Sprintf("exit Code %d", event.ExitCode))

				if event.Signal != 0 {
					parts = append(parts, fmt.Sprintf("signal %d", event.Signal))
				}

				if event.Message != "" {
					parts = append(parts, fmt.Sprintf("exit message %q", event.Message))
				}
				desc = strings.Join(parts, ", ")
			case nomad.TaskNotRestarting:
				if event.RestartReason != "" {
					desc = event.RestartReason
				} else {
					desc = "the task exceeded restart policy"
				}
			case nomad.TaskSiblingFailed:
				if event.FailedSibling != "" {
					desc = fmt.Sprintf("task's sibling %q failed", event.FailedSibling)
				} else {
					desc = "task's sibling failed"
				}
			case nomad.TaskLeaderDead:
				desc = "leader task in group is dead"
			}

			// If we have matched and have an updated desc then log the appropriate
			// information.
			if desc != "" {
				logging.Error("levant/failure_inspector: alloc %s incurred event %s because %s",
					allocID, strings.ToLower(event.Type), strings.TrimSpace(desc))
			} else {
				logging.Error("levant/failure_inspector: alloc %s logged for failure; event_type: %s; message: %s",
					allocID,
					strings.ToLower(event.Type),
					strings.ToLower(event.DisplayMessage))
			}
		}
	}
}
