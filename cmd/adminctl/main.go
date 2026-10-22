// Command adminctl manages the accounts of the Reading Trainer from the
// command line. It is the safe way to create the very first (main)
// administrator on a fresh installation.
//
// Usage:
//
//	adminctl create-admin --username root --password 'secret' [--display-name "Root"]
//	adminctl create-user  --username alice --password 'secret' [--role admin|user]
//	adminctl passwd       --username alice --password 'newsecret'
//	adminctl promote      --username alice
//	adminctl demote       --username alice
//	adminctl list
//
// The command reads DB_PATH from the environment, exactly like the server.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"readingtrainer/internal/auth"
	"readingtrainer/internal/config"
	"readingtrainer/internal/store"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return fmt.Errorf("a command is required")
	}

	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	username := flags.String("username", "", "username of the account")
	password := flags.String("password", "", "new password")
	displayName := flags.String("display-name", "", "display name of the account")
	role := flags.String("role", store.RoleUser, "account role: admin or user")

	if err := flags.Parse(args[1:]); err != nil {
		return err
	}

	cfg := config.Load()
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		return err
	}

	switch command {
	case "create-admin":
		return createAdmin(ctx, st, *username, *password, *displayName)
	case "create-user":
		return createUser(ctx, st, *username, *password, *displayName, *role)
	case "passwd":
		return setPassword(ctx, st, *username, *password)
	case "promote":
		return changeRole(ctx, st, *username, store.RoleAdmin)
	case "demote":
		return changeRole(ctx, st, *username, store.RoleUser)
	case "list":
		return listUsers(ctx, st)
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func createAdmin(ctx context.Context, st *store.Store, username, password, displayName string) error {
	if strings.TrimSpace(username) == "" || password == "" {
		return fmt.Errorf("--username and --password are required")
	}
	if len(password) < 8 {
		return fmt.Errorf("the password must be at least 8 characters long")
	}

	hasRoot, err := st.HasRootAdmin(ctx)
	if err != nil {
		return err
	}
	if hasRoot {
		return fmt.Errorf("a main administrator already exists, use create-user instead")
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	user, err := st.CreateUser(ctx, username, displayName, hash, store.RoleAdmin, true)
	if err != nil {
		return err
	}

	fmt.Printf("main administrator %q created (id %d)\n", user.Username, user.ID)
	return nil
}

func createUser(ctx context.Context, st *store.Store, username, password, displayName, role string) error {
	if strings.TrimSpace(username) == "" || password == "" {
		return fmt.Errorf("--username and --password are required")
	}
	if len(password) < 8 {
		return fmt.Errorf("the password must be at least 8 characters long")
	}
	if role != store.RoleAdmin && role != store.RoleUser {
		return fmt.Errorf("--role must be %q or %q", store.RoleAdmin, store.RoleUser)
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	user, err := st.CreateUser(ctx, username, displayName, hash, role, false)
	if err != nil {
		return err
	}

	fmt.Printf("account %q created with the role %q (id %d)\n", user.Username, user.Role, user.ID)
	return nil
}

func setPassword(ctx context.Context, st *store.Store, username, password string) error {
	if strings.TrimSpace(username) == "" || password == "" {
		return fmt.Errorf("--username and --password are required")
	}
	if len(password) < 8 {
		return fmt.Errorf("the password must be at least 8 characters long")
	}

	user, err := st.GetUserByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("account %q not found", username)
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := st.UpdateUserPassword(ctx, user.ID, hash); err != nil {
		return err
	}

	// Force every session of the account to end.
	if err := st.DeleteUserSessions(ctx, user.ID); err != nil {
		return err
	}

	fmt.Printf("password of %q updated\n", user.Username)
	return nil
}

func changeRole(ctx context.Context, st *store.Store, username, role string) error {
	if strings.TrimSpace(username) == "" {
		return fmt.Errorf("--username is required")
	}

	user, err := st.GetUserByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("account %q not found", username)
	}
	if user.IsRoot {
		return fmt.Errorf("the main administrator keeps its role")
	}
	if err := st.UpdateUserRole(ctx, user.ID, role); err != nil {
		return err
	}

	fmt.Printf("account %q now has the role %q\n", user.Username, role)
	return nil
}

func listUsers(ctx context.Context, st *store.Store) error {
	users, err := st.ListUsers(ctx)
	if err != nil {
		return err
	}
	if len(users) == 0 {
		fmt.Println("no accounts yet")
		return nil
	}

	fmt.Printf("%-5s %-20s %-10s %-8s %s\n", "ID", "USERNAME", "ROLE", "MAIN", "CREATED")
	for _, u := range users {
		main := ""
		if u.IsRoot {
			main = "yes"
		}
		fmt.Printf("%-5d %-20s %-10s %-8s %s\n", u.ID, u.Username, u.Role, main,
			u.CreatedAt.Format("2006-01-02 15:04"))
	}
	return nil
}

func usage() {
	fmt.Fprint(os.Stderr, `adminctl — manage the accounts of the Reading Trainer

Commands:
  create-admin --username <name> --password <secret> [--display-name "Name"]
      Create the main (absolute) administrator. Only possible once.

  create-user --username <name> --password <secret> [--role admin|user] [--display-name "Name"]
      Create a normal account, or an administrator account.

  passwd --username <name> --password <secret>
      Set a new password and end every session of that account.

  promote --username <name>      Give an account the administrator role
  demote --username <name>       Take the administrator role away
  list                            Show every account

The database is taken from the DB_PATH environment variable.
`)
}
