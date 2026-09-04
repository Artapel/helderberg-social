package main

import (
	"fmt"
	"net/http"
	"strings"
)

// The admin's side of member accounts: a list with filters, a per-member
// page with their events, and the actions the console's /admin/do route
// dispatches with the member-* prefix.

type membersData struct {
	Filter, Q                 string
	Members                   []Member
	Total, Page, Pages        int
	Active, Pending, Disabled int
	EventsFromMembers         int
	AccountURL                string
	RegOn                     bool
}

func (a *App) membersPage(w http.ResponseWriter, r *http.Request) {
	d := membersData{Filter: r.URL.Query().Get("f"), Q: clean(r.URL.Query().Get("q"), 80), AccountURL: a.cfg.APIURL, RegOn: a.settingBool("registrations_on")}
	where, args := "1=1", []any{}
	switch d.Filter {
	case "active":
		where = "status = 'active' AND verified_at IS NOT NULL"
	case "pending":
		where = "verified_at IS NULL"
	case "disabled":
		where = "status = 'disabled'"
	}
	if d.Q != "" {
		where += " AND (email LIKE ? OR name LIKE ?)"
		args = append(args, "%"+d.Q+"%", "%"+d.Q+"%")
	}
	d.Total = a.count(`SELECT COUNT(*) FROM members WHERE `+where, args...)
	var off int
	d.Page, off = pageOf(r)
	d.Pages = (d.Total + pageSize - 1) / pageSize
	d.Members = a.members(where, pageSize, off, args...)
	d.Active = a.count(`SELECT COUNT(*) FROM members WHERE status = 'active' AND verified_at IS NOT NULL`)
	d.Pending = a.count(`SELECT COUNT(*) FROM members WHERE verified_at IS NULL`)
	d.Disabled = a.count(`SELECT COUNT(*) FROM members WHERE status = 'disabled'`)
	d.EventsFromMembers = a.count(`SELECT COUNT(*) FROM events WHERE member_id IS NOT NULL`)
	a.renderConsole(w, r, "p_members", "Members", d)
}

type memberViewData struct {
	M        *Member
	Events   []Event
	Sessions int
}

func (a *App) memberViewPage(w http.ResponseWriter, r *http.Request) {
	var id int64
	fmt.Sscan(r.URL.Query().Get("id"), &id)
	m := a.memberByID(id)
	if m == nil {
		a.back(w, r, "/admin/members", "No such member.", true)
		return
	}
	evs, _ := a.queryEvents(`member_id = ?`, id)
	d := memberViewData{M: m, Events: evs, Sessions: a.count(`SELECT COUNT(*) FROM member_sessions WHERE member_id = ? AND revoked = 0 AND expires_at > ?`, id, now())}
	a.renderConsole(w, r, "p_member_view", "Member: "+m.Name, d)
}

func (a *App) memberAction(r *http.Request, action, idStr string) (string, error) {
	var id int64
	fmt.Sscan(idStr, &id)
	m := a.memberByID(id)
	if m == nil {
		return "", fmt.Errorf("no such member")
	}
	target := fmt.Sprint(id)
	switch action {
	case "member-disable":
		_, err := a.db.Exec(`UPDATE members SET status = 'disabled' WHERE id = ?`, id)
		if err != nil {
			return "", err
		}
		a.revokeMemberSessions(id)
		a.audit(r, "member.disable", target, "")
		return fmt.Sprintf("%s is disabled and signed out everywhere.", m.Name), nil
	case "member-enable":
		_, err := a.db.Exec(`UPDATE members SET status = 'active' WHERE id = ?`, id)
		if err != nil {
			return "", err
		}
		a.lock.clear(emailHash(m.Email))
		a.audit(r, "member.enable", target, "")
		return fmt.Sprintf("%s can sign in again.", m.Name), nil
	case "member-verify":
		_, err := a.db.Exec(`UPDATE members SET verified_at = COALESCE(verified_at, ?) WHERE id = ?`, now(), id)
		if err != nil {
			return "", err
		}
		a.audit(r, "member.verify_admin", target, "")
		return fmt.Sprintf("%s's email is marked confirmed.", m.Name), nil
	case "member-resend":
		if m.VerifiedAt != "" {
			return "That address is already confirmed.", nil
		}
		a.sendMemberVerify(m, "/account")
		a.audit(r, "member.resend", target, "")
		return "Confirmation email sent again.", nil
	case "member-signout":
		a.revokeMemberSessions(id)
		a.audit(r, "member.signout", target, "")
		return fmt.Sprintf("%s is signed out on every device.", m.Name), nil
	case "member-delete", "member-block":
		if r.PostForm.Get("confirm") != "yes" {
			return "", fmt.Errorf("tick the box to confirm")
		}
		if action == "member-block" {
			if err := a.addBlock("email", emailHash(m.Email), "blocked from member #"+target); err != nil {
				return "", err
			}
		}
		a.deleteMember(id, r, "admin")
		if action == "member-block" {
			a.audit(r, "member.block", target, "")
			return "Address blocked and the account deleted.", nil
		}
		return "Account deleted.", nil
	}
	return "", fmt.Errorf("unknown member action %q", strings.TrimPrefix(action, "member-"))
}
