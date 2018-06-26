package packet

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPacketVolumeIDToName(t *testing.T) {
	name := VolumeIDToName("3ee59355-a51a-42a8-b848-86626cc532f0")
	assert.Equal(t, name, "volume-3ee59355")
}
