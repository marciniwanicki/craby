package agent

import (
	"testing"
)

func TestParsePlan_Simple(t *testing.T) {
	content := `Here is my analysis:
<plan>
  <intent>Answer a simple math question</intent>
  <complexity>simple</complexity>
  <needs_tools>false</needs_tools>
  <context>
    <item>User is asking about basic arithmetic</item>
  </context>
  <steps></steps>
</plan>
Done.`

	plan, err := ParsePlan(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.Intent != "Answer a simple math question" {
		t.Errorf("expected intent 'Answer a simple math question', got %q", plan.Intent)
	}

	if plan.Complexity != ComplexitySimple {
		t.Errorf("expected complexity 'simple', got %q", plan.Complexity)
	}

	if plan.NeedsTools {
		t.Error("expected needs_tools to be false")
	}

	if len(plan.Context) != 1 {
		t.Fatalf("expected 1 context item, got %d", len(plan.Context))
	}

	if plan.Context[0] != "User is asking about basic arithmetic" {
		t.Errorf("unexpected context item: %q", plan.Context[0])
	}

	if len(plan.Steps) != 0 {
		t.Errorf("expected 0 steps, got %d", len(plan.Steps))
	}
}

func TestParsePlan_SingleTool(t *testing.T) {
	content := `<plan>
  <intent>Get the current time</intent>
  <complexity>tool</complexity>
  <needs_tools>true</needs_tools>
  <context>
    <item>User wants to know the time</item>
  </context>
  <steps>
    <step id="step_1">
      <tool>shell</tool>
      <purpose>Get current date and time</purpose>
      <args>
        <arg name="command">date</arg>
      </args>
    </step>
  </steps>
</plan>`

	plan, err := ParsePlan(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.Complexity != ComplexityTool {
		t.Errorf("expected complexity 'tool', got %q", plan.Complexity)
	}

	if !plan.NeedsTools {
		t.Error("expected needs_tools to be true")
	}

	if len(plan.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(plan.Steps))
	}

	step := plan.Steps[0]
	if step.ID != "step_1" {
		t.Errorf("expected step ID 'step_1', got %q", step.ID)
	}

	if step.Tool != "shell" {
		t.Errorf("expected tool 'shell', got %q", step.Tool)
	}

	if step.Purpose != "Get current date and time" {
		t.Errorf("expected purpose 'Get current date and time', got %q", step.Purpose)
	}

	args := step.ArgsMap()
	if args["command"] != "date" {
		t.Errorf("expected command 'date', got %v", args["command"])
	}
}

func TestParsePlan_MultiStep(t *testing.T) {
	content := `<plan>
  <intent>List files and show first one</intent>
  <complexity>multi_step</complexity>
  <needs_tools>true</needs_tools>
  <context>
    <item>User wants to see directory contents</item>
    <item>Then view specific file</item>
  </context>
  <steps>
    <step id="step_1">
      <tool>shell</tool>
      <purpose>List files in directory</purpose>
      <args>
        <arg name="command">ls</arg>
      </args>
    </step>
    <step id="step_2" depends_on="step_1">
      <tool>shell</tool>
      <purpose>Show contents of first file</purpose>
      <args>
        <arg name="command">head -10 file.txt</arg>
      </args>
    </step>
  </steps>
</plan>`

	plan, err := ParsePlan(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.Complexity != ComplexityMultiStep {
		t.Errorf("expected complexity 'multi_step', got %q", plan.Complexity)
	}

	if len(plan.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(plan.Steps))
	}

	if plan.Steps[0].DependsOn != "" {
		t.Errorf("step_1 should not have depends_on, got %q", plan.Steps[0].DependsOn)
	}

	if plan.Steps[1].DependsOn != "step_1" {
		t.Errorf("step_2 should depend on step_1, got %q", plan.Steps[1].DependsOn)
	}
}

func TestParsePlan_NoPlanBlock(t *testing.T) {
	content := "This response has no plan block"

	_, err := ParsePlan(content)
	if err == nil {
		t.Error("expected error for missing plan block")
	}
}

func TestParsePlan_InvalidComplexity(t *testing.T) {
	content := `<plan>
  <intent>Test</intent>
  <complexity>unknown</complexity>
  <needs_tools>false</needs_tools>
  <context></context>
  <steps></steps>
</plan>`

	_, err := ParsePlan(content)
	if err == nil {
		t.Error("expected error for invalid complexity")
	}
}

func TestParsePlan_NeedsToolsButNoSteps(t *testing.T) {
	content := `<plan>
  <intent>Test</intent>
  <complexity>tool</complexity>
  <needs_tools>true</needs_tools>
  <context></context>
  <steps></steps>
</plan>`

	_, err := ParsePlan(content)
	if err == nil {
		t.Error("expected error when needs_tools is true but no steps provided")
	}
}

func TestPlanStep_ArgsMap(t *testing.T) {
	step := PlanStep{
		Args: []PlanArg{
			{Name: "command", Value: "  ls -la  "},
			{Name: "timeout", Value: "30"},
		},
	}

	args := step.ArgsMap()

	if args["command"] != "ls -la" {
		t.Errorf("expected trimmed 'ls -la', got %q", args["command"])
	}

	if args["timeout"] != "30" {
		t.Errorf("expected '30', got %q", args["timeout"])
	}
}
