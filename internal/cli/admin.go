// Package cli implements the `homeview admin ...` subcommands. They talk to
// the database directly, so they work from `docker compose exec` without any
// secret ever being written to docker-compose.yml.
package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/mail"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/andregoncalvespires/homeview-dash/internal/auth"
	"github.com/andregoncalvespires/homeview-dash/internal/config"
	"github.com/andregoncalvespires/homeview-dash/internal/i18n"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

// IO bundles the streams used by a command.
type IO struct {
	In  *bufio.Reader
	Out io.Writer
	Err io.Writer
}

// Admin runs an `admin` subcommand and returns the process exit code.
func Admin(ctx context.Context, args []string, st store.Store, cfg config.Config, io IO) int {
	pt := i18n.Normalize(cfg.DefaultLang) == i18n.PT
	tr := func(p, e string) string {
		if pt {
			return p
		}
		return e
	}
	if len(args) == 0 {
		fmt.Fprintln(io.Err, tr(
			"uso: homeview admin <create|login-link|list>",
			"usage: homeview admin <create|login-link|list>"))
		return 2
	}
	cmd, rest := args[0], args[1:]

	fs := flag.NewFlagSet("admin "+cmd, flag.ContinueOnError)
	fs.SetOutput(io.Err)
	email := fs.String("email", "", "e-mail")
	name := fs.String("name", "", tr("nome", "name"))
	if err := fs.Parse(rest); err != nil {
		return 2
	}

	switch cmd {
	case "create":
		return create(ctx, st, cfg, io, tr, *email, *name)
	case "login-link":
		return loginLink(ctx, st, cfg, io, tr, *email)
	case "list":
		return list(ctx, st, io, tr)
	default:
		fmt.Fprintf(io.Err, tr("comando desconhecido: %q\n", "unknown command: %q\n"), cmd)
		return 2
	}
}

func prompt(io IO, label string) string {
	fmt.Fprint(io.Out, label)
	line, _ := io.In.ReadString('\n')
	return strings.TrimSpace(line)
}

func create(ctx context.Context, st store.Store, cfg config.Config, io IO, tr func(p, e string) string, email, name string) int {
	if email == "" {
		email = prompt(io, tr("E-mail do administrador: ", "Administrator e-mail: "))
	}
	email = store.NormalizeEmail(email)
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email {
		fmt.Fprintln(io.Err, tr("E-mail inválido.", "Invalid e-mail address."))
		return 1
	}
	if name == "" {
		name = prompt(io, tr("Nome: ", "Name: "))
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 80 {
		fmt.Fprintln(io.Err, tr("Nome inválido (1 a 80 caracteres).", "Invalid name (1 to 80 characters)."))
		return 1
	}
	if _, err := st.CreateUser(ctx, email, name, store.RoleAdmin); err != nil {
		if errors.Is(err, store.ErrExists) {
			fmt.Fprintln(io.Err, tr("Já existe um usuário com esse e-mail.", "A user with that e-mail already exists."))
		} else {
			fmt.Fprintln(io.Err, "error:", err)
		}
		return 1
	}
	fmt.Fprintf(io.Out, tr(
		"Administrador %s criado.\nAbra %s/login e entre com esse e-mail (o código chega por e-mail).\n",
		"Administrator %s created.\nOpen %s/login and sign in with that e-mail (the code arrives by e-mail).\n"),
		email, cfg.BaseURL)
	return 0
}

func loginLink(ctx context.Context, st store.Store, cfg config.Config, io IO, tr func(p, e string) string, email string) int {
	if email == "" {
		email = prompt(io, tr("E-mail do usuário: ", "User e-mail: "))
	}
	email = store.NormalizeEmail(email)
	u, err := st.UserByEmail(ctx, email)
	if errors.Is(err, store.ErrNotFound) {
		fmt.Fprintln(io.Err, tr("Usuário não encontrado.", "User not found."))
		return 1
	}
	if err != nil {
		fmt.Fprintln(io.Err, "error:", err)
		return 1
	}
	svc := auth.New(st, nil, nil, auth.DefaultConfig())
	token, err := svc.CreateLinkToken(ctx, u.ID)
	if err != nil {
		fmt.Fprintln(io.Err, "error:", err)
		return 1
	}
	minutes := int(auth.DefaultConfig().LinkTTL.Minutes())
	fmt.Fprintf(io.Out, tr(
		"Link de acesso único para %s (vale %d minutos, uso único):\n\n%s/auth/link?token=%s\n",
		"One-time sign-in link for %s (valid %d minutes, single use):\n\n%s/auth/link?token=%s\n"),
		u.Email, minutes, cfg.BaseURL, url.QueryEscape(token))
	return 0
}

func list(ctx context.Context, st store.Store, io IO, tr func(p, e string) string) int {
	users, err := st.ListUsers(ctx)
	if err != nil {
		fmt.Fprintln(io.Err, "error:", err)
		return 1
	}
	if len(users) == 0 {
		fmt.Fprintln(io.Out, tr("Nenhum usuário cadastrado.", "No users yet."))
		return 0
	}
	for _, u := range users {
		fmt.Fprintf(io.Out, "%-8s %-40s %s\n", u.Role, u.Email, u.Name)
	}
	return 0
}
