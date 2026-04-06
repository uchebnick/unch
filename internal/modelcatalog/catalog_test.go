package modelcatalog

import "testing"

func TestKnownInstallTargets(t *testing.T) {
	t.Parallel()

	targets := KnownInstallTargets()
	if len(targets) < 2 {
		t.Fatalf("KnownInstallTargets() = %d, want at least 2", len(targets))
	}

	if got := DefaultInstallTarget().ID; got != "embeddinggemma" {
		t.Fatalf("DefaultInstallTarget().ID = %q", got)
	}

	qwen, ok := ResolveInstallTarget("qwen3")
	if !ok || qwen.ID != "qwen3" {
		t.Fatalf("ResolveInstallTarget(qwen3) = (%#v, %v)", qwen, ok)
	}

	gemmaByPath, ok := RecognizeInstallTargetForPath("/tmp/embeddinggemma-300m.gguf")
	if !ok || gemmaByPath.ID != "embeddinggemma" {
		t.Fatalf("RecognizeInstallTargetForPath(gemma) = (%#v, %v)", gemmaByPath, ok)
	}
}

func TestKnownInstallTargetsReturnClonedAliases(t *testing.T) {
	t.Parallel()

	targets := KnownInstallTargets()
	targets[0].Aliases[0] = "mutated"

	freshTargets := KnownInstallTargets()
	if freshTargets[0].Aliases[0] == "mutated" {
		t.Fatalf("KnownInstallTargets() returned shared alias slices")
	}
}
