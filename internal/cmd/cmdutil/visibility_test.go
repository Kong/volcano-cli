package cmdutil

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

func parseVisibilityFlags(t *testing.T, args ...string) *VisibilityFlags {
	t.Helper()
	var flags VisibilityFlags
	set := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Register(set, "who can invoke")
	require.NoError(t, set.Parse(args))
	return &flags
}

func TestVisibilityFlagsSelectALevel(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want apiclient.FunctionVisibility
	}{
		{args: []string{"--visibility", "private"}, want: apiclient.FunctionVisibilityPrivate},
		{args: []string{"--visibility", " Authenticated "}, want: apiclient.FunctionVisibilityAuthenticated},
		{args: []string{"--visibility=public"}, want: apiclient.FunctionVisibilityPublic},
		{args: []string{"--public"}, want: apiclient.FunctionVisibilityPublic},
	} {
		flags := parseVisibilityFlags(t, tc.args...)
		visibility, ok, err := flags.Visibility()
		require.NoError(t, err)
		assert.True(t, ok)
		assert.True(t, flags.Set())
		assert.Equal(t, tc.want, visibility)
	}
}

func TestVisibilityFlagsLeftOutSelectNothing(t *testing.T) {
	flags := parseVisibilityFlags(t)
	_, ok, err := flags.Visibility()
	require.NoError(t, err)
	assert.False(t, ok)
	assert.False(t, flags.Set())
}

func TestVisibilityFlagsRefusals(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{args: []string{"--visibility", "everyone"}, want: `invalid --visibility "everyone"`},
		{args: []string{"--public", "--visibility", "public"}, want: "use one or the other"},
		{args: []string{"--private"}, want: "--private is no longer accepted"},
		{args: []string{"--private", "--visibility", "private"}, want: "--private is no longer accepted"},
	} {
		flags := parseVisibilityFlags(t, tc.args...)
		_, _, err := flags.Visibility()
		require.ErrorContains(t, err, tc.want)
		assert.True(t, flags.Set())
	}
}
