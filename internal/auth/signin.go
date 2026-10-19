package auth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/confirm"
)

// OfferSignIn runs when a command needs a login and none is saved. On a
// terminal it asks whether the user has an account, runs browser login or
// signup, and saves the login, so the command can carry on. Without a
// terminal, or in CI, it returns config.ErrNotAuthenticated unchanged: a
// script or agent never waits on a prompt.
func (s Service) OfferSignIn(ctx context.Context, in io.Reader, out io.Writer) error {
	if !canOfferSignIn(in, out) {
		return config.ErrNotAuthenticated
	}
	return s.offerSignIn(ctx, in, out)
}

func (s Service) offerSignIn(ctx context.Context, in io.Reader, out io.Writer) error {
	cfg, err := s.sessions.Config()
	if err != nil {
		return err
	}
	reader := bufio.NewReader(in)
	fmt.Fprintln(out, "This command needs a Volcano account.")
	hasAccount, err := ask(reader, out, "Do you already have one? [y/N]: ")
	if errors.Is(err, io.EOF) {
		// The input closed without an answer: nobody is there to sign up.
		fmt.Fprintln(out)
		return config.ErrNotAuthenticated
	}
	if err != nil {
		return err
	}

	var credentials Credentials
	if strings.EqualFold(hasAccount, "y") || strings.EqualFold(hasAccount, "yes") {
		credentials, err = s.LoginWithBrowser(ctx, cfg, out)
	} else {
		email := GitEmail(ctx, s.deps)
		if email == "" {
			if email, err = ask(reader, out, "Email for your new account: "); err != nil && !errors.Is(err, io.EOF) {
				return err
			}
		}
		if email == "" {
			return errors.New("signup needs an email address")
		}
		credentials, err = s.Signup(ctx, cfg, email, out)
	}
	if err != nil {
		return err
	}
	if err := SaveCredentials(credentials); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}
	fmt.Fprintln(out, "Signed in. Continuing.")
	return nil
}

// canOfferSignIn reports whether a person is there to answer: both ends are
// terminals and nothing marks the run as non-interactive.
func canOfferSignIn(in io.Reader, out io.Writer) bool {
	if confirm.MarkedNonInteractive() {
		return false
	}
	outFile, ok := out.(*os.File)
	return ok && confirm.CanPrompt(in) && confirm.CanPrompt(outFile)
}

// ask prints prompt and returns the trimmed answer. It returns io.EOF when
// the input closed before any answer.
func ask(r *bufio.Reader, w io.Writer, prompt string) (string, error) {
	fmt.Fprint(w, prompt)
	line, err := r.ReadString('\n')
	if errors.Is(err, io.EOF) && strings.TrimSpace(line) == "" {
		return "", io.EOF
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
