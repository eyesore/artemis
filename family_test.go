package artemis

import "testing"

func testFamilyClientHubMismatch(t *testing.T) {
	h1 := createTestHub(t, "h1")
	f1 := createTestFamily(t, "f1", h1)
	f2 := createTestFamily(t, "f2", nil)
	f3 := createTestFamily(t, "f3", nil)
	c1 := createTestClient(t, "c1", nil)

	if err := c1.Join(f1); err != ErrHubMismatch {
		t.Error("Expected hub mismatch error, but none returned.")
	}
	if err := c1.Join(f2, f3); err != nil {
		t.Error("Client should be able to join these families without issue.")
	}

	c1.Leave(f2)
	c1.Leave(f3)

	err := c1.Join(f1, f2, f3)
	if err != ErrHubMismatch {
		t.Error("Expected hub mismatch when joining all 3 families, but none returned.")
	}
	if c1.BelongsTo(f1) || !c1.BelongsTo(f2) || !c1.BelongsTo(f3) {
		t.Error("Resulting family membership for c1 is not correct.")
	}
	cleanup()
}

func testFamilyJoinLeave(t *testing.T) {
	c1 := createTestClient(t, "c1", nil)
	f1 := createTestFamily(t, "f1", nil)
	f2 := createTestFamily(t, "f2", nil)

	c1.Join(f1, f2)
	if !c1.BelongsTo(f1) || !c1.BelongsTo(f2) {
		t.Fatal("c1 did not correctly join families.")
	}

	c1.Leave(f2)
	if !c1.BelongsTo(f1) {
		t.Error("c1 should still belong to f1")
	}
	if c1.BelongsTo(f2) {
		t.Error("c1 should no longer belong to f2")
	}
	cleanup()
}
