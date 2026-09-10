package variable

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReservedNamesReturnsCopy(t *testing.T) {
	names := ReservedNames()
	names[0] = "CHANGED"
	assert.NotEqual(t, "CHANGED", ReservedNames()[0])
}
