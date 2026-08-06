package handlers

import "testing"

// EIP-8312 frames use mode 5 (EIP-7906 holds 3; 4 is reserved for EIP-8288), so an
// unlabelled UTXO frame renders as RESERVED(5) to anyone reading the explorer.
func TestFrameModeName(t *testing.T) {
	for mode, want := range map[uint8]string{
		0: "DEFAULT",
		1: "VERIFY",
		2: "SENDER",
		3: "POST_TX",
		5: "UTXO",
		4: "RESERVED(4)",
		9: "RESERVED(9)",
	} {
		if got := frameModeName(mode); got != want {
			t.Errorf("frameModeName(%d) = %q, want %q", mode, got, want)
		}
	}
}
