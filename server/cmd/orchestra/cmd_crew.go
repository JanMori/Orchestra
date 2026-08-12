package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/JanMori/Orchestra/server/internal/cli"
)

var crewCmd = &cobra.Command{
	Use:   "crew",
	Short: "Work with crews",
}

// ── List ────────────────────────────────────────────────────────────────────

var crewListCmd = &cobra.Command{
	Use:   "list",
	Short: "List crews in the workspace",
	Args:  cobra.NoArgs,
	RunE:  runCrewList,
}

func runCrewList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var crews []map[string]any
	if err := client.GetJSON(ctx, "/api/crews", &crews); err != nil {
		return fmt.Errorf("list crews: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, crews)
	}

	if len(crews) == 0 {
		fmt.Fprintln(os.Stderr, "No crews found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tLEADER ID\tMEMBERS")
	for _, s := range crews {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			strVal(s, "id"), strVal(s, "name"), strVal(s, "leader_id"),
			memberCountDisplay(s))
	}
	return w.Flush()
}

func memberCountDisplay(m map[string]any) string {
	v, ok := m["member_count"]
	if !ok || v == nil {
		return "-"
	}
	n, ok := v.(float64)
	if !ok || n <= 0 {
		return "-"
	}
	return strconv.Itoa(int(n))
}

// ── Get ─────────────────────────────────────────────────────────────────────

var crewGetCmd = &cobra.Command{
	Use:   "get <crew-id>",
	Short: "Get crew details",
	Args:  exactArgs(1),
	RunE:  runCrewGet,
}

func runCrewGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var crew map[string]any
	if err := client.GetJSON(ctx, "/api/crews/"+args[0], &crew); err != nil {
		return fmt.Errorf("get crew: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, crew)
	}

	fmt.Printf("ID:           %s\n", strVal(crew, "id"))
	fmt.Printf("Name:         %s\n", strVal(crew, "name"))
	fmt.Printf("Description:  %s\n", strVal(crew, "description"))
	fmt.Printf("Leader ID:    %s\n", strVal(crew, "leader_id"))
	fmt.Printf("Created:      %s\n", strVal(crew, "created_at"))
	if inst := strVal(crew, "instructions"); inst != "" {
		fmt.Printf("Instructions: %s\n", inst)
	}
	return nil
}

// ── Create ──────────────────────────────────────────────────────────────────

var crewCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new crew",
	Args:  cobra.NoArgs,
	RunE:  runCrewCreate,
}

func runCrewCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	leader, _ := cmd.Flags().GetString("leader")
	if leader == "" {
		return fmt.Errorf("--leader is required (agent name or ID)")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	leaderID, err := resolveAgent(ctx, client, leader)
	if err != nil {
		return fmt.Errorf("resolve leader: %w", err)
	}

	body := map[string]any{
		"name":      name,
		"leader_id": leaderID,
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/crews", body, &result); err != nil {
		return fmt.Errorf("create crew: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Crew created: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

// ── Update ──────────────────────────────────────────────────────────────────

var crewUpdateCmd = &cobra.Command{
	Use:   "update <crew-id>",
	Short: "Update a crew",
	Args:  exactArgs(1),
	RunE:  runCrewUpdate,
}

func runCrewUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		v, _ := cmd.Flags().GetString("name")
		body["name"] = v
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	if cmd.Flags().Changed("instructions") {
		v, _ := cmd.Flags().GetString("instructions")
		body["instructions"] = v
	}
	if cmd.Flags().Changed("leader") {
		v, _ := cmd.Flags().GetString("leader")
		leaderID, err := resolveAgent(ctx, client, v)
		if err != nil {
			return fmt.Errorf("resolve leader: %w", err)
		}
		body["leader_id"] = leaderID
	}
	if cmd.Flags().Changed("avatar-url") {
		v, _ := cmd.Flags().GetString("avatar-url")
		body["avatar_url"] = v
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --name, --description, --instructions, --leader")
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/crews/"+args[0], body, &result); err != nil {
		return fmt.Errorf("update crew: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Crew updated: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

// ── Delete ──────────────────────────────────────────────────────────────────

var crewDeleteCmd = &cobra.Command{
	Use:   "delete <crew-id>",
	Short: "Delete (archive) a crew",
	Args:  exactArgs(1),
	RunE:  runCrewDelete,
}

func runCrewDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/crews/"+args[0]); err != nil {
		return fmt.Errorf("delete crew: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"id": args[0], "deleted": true})
	}
	fmt.Fprintf(os.Stderr, "Crew %s deleted.\n", args[0])
	return nil
}

// ── Members ─────────────────────────────────────────────────────────────────

var crewMemberCmd = &cobra.Command{
	Use:   "member",
	Short: "Work with crew members",
}

var crewMemberListCmd = &cobra.Command{
	Use:   "list <crew-id>",
	Short: "List members of a crew",
	Args:  exactArgs(1),
	RunE:  runCrewMemberList,
}

func runCrewMemberList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var members []map[string]any
	if err := client.GetJSON(ctx, "/api/crews/"+args[0]+"/members", &members); err != nil {
		return fmt.Errorf("list members: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, members)
	}

	if len(members) == 0 {
		fmt.Fprintln(os.Stderr, "No members found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "MEMBER ID\tTYPE\tROLE")
	for _, m := range members {
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			strVal(m, "member_id"), strVal(m, "member_type"), strVal(m, "role"))
	}
	return w.Flush()
}

// ── Member Add ──────────────────────────────────────────────────────────────

var crewMemberAddCmd = &cobra.Command{
	Use:   "add <crew-id>",
	Short: "Add a member to a crew",
	Args:  exactArgs(1),
	RunE:  runCrewMemberAdd,
}

func runCrewMemberAdd(cmd *cobra.Command, args []string) error {
	memberID, _ := cmd.Flags().GetString("member-id")
	memberType, _ := cmd.Flags().GetString("type")
	role, _ := cmd.Flags().GetString("role")

	if memberID == "" {
		return fmt.Errorf("--member-id is required")
	}
	if memberType != "agent" && memberType != "member" {
		return fmt.Errorf("--type must be 'agent' or 'member'")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{
		"member_type": memberType,
		"member_id":   memberID,
		"role":        role,
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/crews/"+args[0]+"/members", body, &result); err != nil {
		return fmt.Errorf("add member: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Member %s added to crew.\n", memberID)
	return nil
}

// ── Member Set Role ─────────────────────────────────────────────────────────

var crewMemberSetRoleCmd = &cobra.Command{
	Use:   "set-role <crew-id>",
	Short: "Change a crew member's role",
	Args:  exactArgs(1),
	RunE:  runCrewMemberSetRole,
}

func runCrewMemberSetRole(cmd *cobra.Command, args []string) error {
	memberID, _ := cmd.Flags().GetString("member-id")
	memberType, _ := cmd.Flags().GetString("member-type")
	role, _ := cmd.Flags().GetString("role")

	if memberID == "" {
		return fmt.Errorf("--member-id is required")
	}
	if memberType != "agent" && memberType != "member" {
		return fmt.Errorf("--member-type must be 'agent' or 'member'")
	}
	if role == "" {
		return fmt.Errorf("--role is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{
		"member_type": memberType,
		"member_id":   memberID,
		"role":        role,
	}

	var result map[string]any
	if err := client.PatchJSON(ctx, "/api/crews/"+args[0]+"/members/role", body, &result); err != nil {
		return fmt.Errorf("set member role: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Fprintf(os.Stderr, "Member %s role updated to %s.\n", memberID, role)
	return nil
}

// ── Member Remove ───────────────────────────────────────────────────────────

var crewMemberRemoveCmd = &cobra.Command{
	Use:   "remove <crew-id>",
	Short: "Remove a member from a crew",
	Args:  exactArgs(1),
	RunE:  runCrewMemberRemove,
}

func runCrewMemberRemove(cmd *cobra.Command, args []string) error {
	memberID, _ := cmd.Flags().GetString("member-id")
	memberType, _ := cmd.Flags().GetString("type")

	if memberID == "" {
		return fmt.Errorf("--member-id is required")
	}
	if memberType != "agent" && memberType != "member" {
		return fmt.Errorf("--type must be 'agent' or 'member'")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{
		"member_type": memberType,
		"member_id":   memberID,
	}

	if err := client.DeleteJSONWithBody(ctx, "/api/crews/"+args[0]+"/members", body); err != nil {
		return fmt.Errorf("remove member: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"crew_id": args[0], "member_id": memberID, "removed": true})
	}
	fmt.Fprintf(os.Stderr, "Member %s removed from crew.\n", memberID)
	return nil
}

// ── Activity ────────────────────────────────────────────────────────────────

var crewActivityCmd = &cobra.Command{
	Use:   "activity <issue-id> <outcome>",
	Short: "Record a crew leader evaluation on an issue",
	Long: `Record the crew leader's evaluation decision for an issue.

Outcome must be one of:
  action     — leader delegated or took action
  no_action  — leader evaluated and decided no action needed
  failed     — leader encountered an error

This command is intended to be called by crew leader agents after each
trigger to record their decision in the issue timeline.`,
	Args: exactArgs(2),
	RunE: runCrewActivity,
}

func runCrewActivity(cmd *cobra.Command, args []string) error {
	issueID := args[0]
	outcome := args[1]

	if outcome != "action" && outcome != "no_action" && outcome != "failed" {
		return fmt.Errorf("invalid outcome %q; valid values: action, no_action, failed", outcome)
	}

	reason, _ := cmd.Flags().GetString("reason")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, issueID)
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	body := map[string]any{
		"outcome": outcome,
		"reason":  reason,
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/crew-evaluated", body, &result); err != nil {
		return fmt.Errorf("record evaluation: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Crew evaluation recorded: %s (issue %s)\n", outcome, issueRef.Display)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	return nil
}

// ── Init ────────────────────────────────────────────────────────────────────

func init() {
	// list
	crewListCmd.Flags().String("output", "table", "Output format: table or json")

	// get
	crewGetCmd.Flags().String("output", "table", "Output format: table or json")

	// create
	crewCreateCmd.Flags().String("name", "", "Crew name (required)")
	crewCreateCmd.Flags().String("description", "", "Crew description")
	crewCreateCmd.Flags().String("leader", "", "Leader agent (name or ID) — required")
	crewCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// update
	crewUpdateCmd.Flags().String("name", "", "New name")
	crewUpdateCmd.Flags().String("description", "", "New description")
	crewUpdateCmd.Flags().String("instructions", "", "New instructions")
	crewUpdateCmd.Flags().String("leader", "", "New leader agent (name or ID)")
	crewUpdateCmd.Flags().String("avatar-url", "", "New avatar URL")
	crewUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// delete
	crewDeleteCmd.Flags().String("output", "table", "Output format: table or json")

	// member list
	crewMemberListCmd.Flags().String("output", "table", "Output format: table or json")

	// member add
	crewMemberAddCmd.Flags().String("member-id", "", "Member or agent ID (required)")
	crewMemberAddCmd.Flags().String("type", "agent", "Member type: agent or member")
	crewMemberAddCmd.Flags().String("role", "member", "Role in the crew")
	crewMemberAddCmd.Flags().String("output", "json", "Output format: table or json")

	// member remove
	crewMemberRemoveCmd.Flags().String("member-id", "", "Member or agent ID (required)")
	crewMemberRemoveCmd.Flags().String("type", "agent", "Member type: agent or member")
	crewMemberRemoveCmd.Flags().String("output", "table", "Output format: table or json")

	// member set-role
	crewMemberSetRoleCmd.Flags().String("member-id", "", "Member or agent ID (required)")
	crewMemberSetRoleCmd.Flags().String("member-type", "agent", "Member type: agent or member")
	crewMemberSetRoleCmd.Flags().String("role", "", "New role in the crew (required)")
	crewMemberSetRoleCmd.Flags().String("output", "json", "Output format: table or json")

	// activity
	crewActivityCmd.Flags().String("reason", "", "Short explanation of the decision")
	crewActivityCmd.Flags().String("output", "table", "Output format: table or json")

	crewMemberCmd.AddCommand(crewMemberListCmd)
	crewMemberCmd.AddCommand(crewMemberAddCmd)
	crewMemberCmd.AddCommand(crewMemberRemoveCmd)
	crewMemberCmd.AddCommand(crewMemberSetRoleCmd)

	crewCmd.AddCommand(crewListCmd)
	crewCmd.AddCommand(crewGetCmd)
	crewCmd.AddCommand(crewCreateCmd)
	crewCmd.AddCommand(crewUpdateCmd)
	crewCmd.AddCommand(crewDeleteCmd)
	crewCmd.AddCommand(crewMemberCmd)
	crewCmd.AddCommand(crewActivityCmd)
}
