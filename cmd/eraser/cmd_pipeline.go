package main

import (
	"fmt"

	"github.com/robindubreuil/eraser/internal/history"
	"github.com/spf13/cobra"
)

func pipelineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Show pipeline status and statistics",
		Long:  "Display the current status of the removal pipeline, including pending tasks and response classifications.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runPipelineStatus()
		},
	}

	return cmd
}

func runPipelineStatus() error {
	store, err := history.NewStore(history.DefaultDBPath())
	if err != nil {
		return fmt.Errorf("failed to open history: %w", err)
	}
	defer store.Close()

	fmt.Println("🔄 Pipeline Status")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Get pipeline stats
	pipelineStats, err := store.GetPipelineStats()
	if err != nil {
		return fmt.Errorf("failed to get pipeline stats: %w", err)
	}

	fmt.Println("📊 Pipeline Stage Breakdown:")
	fmt.Printf("  📧 Email sent:            %d\n", pipelineStats[history.PipelineEmailSent])
	fmt.Printf("  ⏳ Awaiting response:     %d\n", pipelineStats[history.PipelineAwaitingResponse])
	fmt.Printf("  📝 Form required:         %d\n", pipelineStats[history.PipelineFormRequired])
	fmt.Printf("  ✏️  Form filled:           %d\n", pipelineStats[history.PipelineFormFilled])
	fmt.Printf("  🤖 Awaiting CAPTCHA:      %d\n", pipelineStats[history.PipelineAwaitingCaptcha])
	fmt.Printf("  ✅ CAPTCHA solved:        %d\n", pipelineStats[history.PipelineCaptchaSolved])
	fmt.Printf("  🔗 Awaiting confirmation: %d\n", pipelineStats[history.PipelineAwaitingConfirmation])
	fmt.Printf("  ✅ Confirmed:             %d\n", pipelineStats[history.PipelineConfirmed])
	fmt.Printf("  ❌ Rejected:              %d\n", pipelineStats[history.PipelineRejected])
	fmt.Printf("  💥 Failed:                %d\n", pipelineStats[history.PipelineFailed])

	// Get response stats
	responseStats, err := store.GetResponseStats()
	if err != nil {
		fmt.Printf("\n⚠️  Could not get response stats: %v\n", err)
	} else if len(responseStats) > 0 {
		fmt.Println()
		fmt.Println("📬 Response Classification:")
		for responseType, count := range responseStats {
			fmt.Printf("  %s: %d\n", responseType, count)
		}
	}

	// Get pending tasks
	pending, completed, skipped, err := store.GetPendingTaskStats()
	if err != nil {
		fmt.Printf("\n⚠️  Could not get task stats: %v\n", err)
	} else if pending+completed+skipped > 0 {
		fmt.Println()
		fmt.Println("📋 Pending Tasks:")
		fmt.Printf("  ⏳ Pending:   %d\n", pending)
		fmt.Printf("  ✅ Completed: %d\n", completed)
		fmt.Printf("  ⏭️  Skipped:   %d\n", skipped)
	}

	// Show actionable items
	tasks, err := store.GetPendingTasks("", "pending")
	if err == nil && len(tasks) > 0 {
		fmt.Println()
		fmt.Println("🎯 Action Required:")
		for _, task := range tasks {
			fmt.Printf("  • %s [%s] - %s\n", task.BrokerName, task.TaskType, task.FormURL)
		}
	}

	return nil
}
