package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/panel"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/panelinstall"
)

// MinPanelPassword is the shortest password the panels accept.
const MinPanelPassword = 8

// PanelAdmin returns the user whose password `panel password` changes by
// default: the admin the installer created, else the first root admin.
func (a *App) PanelAdmin(ctx context.Context) (panel.User, error) {
	return a.panelUser(ctx, "")
}

func (a *App) panelUser(ctx context.Context, email string) (panel.User, error) {
	users, err := a.Panel().Users(ctx)
	if err != nil {
		return panel.User{}, err
	}
	if email == "" {
		email, _, _ = panelinstall.AdminLogin()
	}
	for _, u := range users {
		if email != "" && strings.EqualFold(u.Email, email) {
			return u, nil
		}
	}
	if email != "" {
		return panel.User{}, fmt.Errorf("no panel user with the email %s", email)
	}
	for _, u := range users {
		if u.RootAdmin {
			return u, nil
		}
	}
	return panel.User{}, errors.New("the panel has no admin user")
}

// SetPanelPassword changes a panel user's password (default: the admin) and
// keeps the installer's record of the admin login current.
func (a *App) SetPanelPassword(ctx context.Context, email, password string) (panel.User, error) {
	if len(password) < MinPanelPassword {
		return panel.User{}, fmt.Errorf("the password needs at least %d characters", MinPanelPassword)
	}
	u, err := a.panelUser(ctx, email)
	if err != nil {
		return u, err
	}
	if err := a.Panel().SetUserPassword(ctx, u.ID, password); err != nil {
		return u, err
	}
	if adminEmail, _, _ := panelinstall.AdminLogin(); strings.EqualFold(adminEmail, u.Email) {
		if err := panelinstall.SetAdminPassword(password); err != nil {
			return u, err
		}
	}
	return u, nil
}
