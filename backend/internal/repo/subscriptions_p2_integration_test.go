package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/coxpanel/backend/internal/db"
	"github.com/coxpanel/backend/internal/models"
)

func prepareP2IntegrationSchema(t *testing.T, resource *p1IntegrationDB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), p1IntegrationTimeout)
	defer cancel()
	if err := db.Migrate(ctx, resource.db); err != nil {
		t.Fatal("real P2 migrations failed")
	}
	if err := db.Migrate(ctx, resource.db); err != nil {
		t.Fatal("repeat P2 migrations were not idempotent")
	}
	rows, err := resource.db.QueryContext(ctx, `SELECT name FROM schema_migrations ORDER BY name`)
	if err != nil {
		t.Fatal("P2 migration ledger could not be read")
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal("P2 migration ledger could not be scanned")
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal("P2 migration ledger read failed")
	}
	expected := []string{
		"0001_init_nodes.up.sql",
		"0002_init_users.up.sql",
		"0003_init_groups.up.sql",
		"0004_init_subscriptions.up.sql",
		"0005_init_traffic.up.sql",
		"0006_user_credentials.up.sql",
		"0007_agent_credentials.up.sql",
		"0008_auth_authorization.up.sql",
		"0009_subscription_inbound_overrides.up.sql",
		"0010_control_plane.up.sql",
		"0011_p2_topology.up.sql",
		"0012_p2_templates_overrides.up.sql",
		"0013_p2_traffic.up.sql",
		"0014_p2_notifications_mail.up.sql",
	}
	if fmt.Sprint(names) != fmt.Sprint(expected) {
		t.Fatalf("P2 migration ledger = %v, want %v", names, expected)
	}
}

func TestP2TemplateSubscriptionOverrideLifecycleIntegration(t *testing.T) {
	resource := openP1IntegrationDB(t)
	prepareP2IntegrationSchema(t, resource)
	ctx := context.Background()

	var userID, otherUserID, groupID, otherGroupID, nodeID, otherNodeID, inboundID, otherInboundID int64
	for index, target := range []*int64{&userID, &otherUserID} {
		if err := resource.db.QueryRowContext(ctx, `
			INSERT INTO users(username,password_hash,role,is_active)
			VALUES ($1,'synthetic-hash','user',TRUE) RETURNING id`, fmt.Sprintf("p2-template-user-%d", index)).Scan(target); err != nil {
			t.Fatal("user fixture setup failed")
		}
	}
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO node_groups(name) VALUES ('p2-template-group') RETURNING id`).Scan(&groupID); err != nil {
		t.Fatal("group fixture setup failed")
	}
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO node_groups(name) VALUES ('p2-template-other-group') RETURNING id`).Scan(&otherGroupID); err != nil {
		t.Fatal("group fixture setup failed")
	}
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO nodes(name,type,public_ip) VALUES ('p2-template-node','managed','node.example') RETURNING id`).Scan(&nodeID); err != nil {
		t.Fatal("node fixture setup failed")
	}
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO nodes(name,type,public_ip) VALUES ('p2-template-other-node','managed','other.example') RETURNING id`).Scan(&otherNodeID); err != nil {
		t.Fatal("node fixture setup failed")
	}
	if _, err := resource.db.ExecContext(ctx, `
		INSERT INTO node_group_members(group_id,node_id) VALUES ($1,$2),($3,$4)`, groupID, nodeID, otherGroupID, otherNodeID); err != nil {
		t.Fatal("group member fixture setup failed")
	}
	if _, err := resource.db.ExecContext(ctx, `INSERT INTO user_node_groups(user_id,group_id) VALUES ($1,$2)`, userID, groupID); err != nil {
		t.Fatal("user group fixture setup failed")
	}
	if err := resource.db.QueryRowContext(ctx, `
		INSERT INTO inbounds(node_id,name,protocol,role,listen_port,config)
		VALUES ($1,'p2-template-inbound','hysteria2','entry',8443,'{"sni":"origin.example"}') RETURNING id`, nodeID).Scan(&inboundID); err != nil {
		t.Fatal("inbound fixture setup failed")
	}
	if err := resource.db.QueryRowContext(ctx, `
		INSERT INTO inbounds(node_id,name,protocol,role,listen_port,config)
		VALUES ($1,'p2-template-other-inbound','hysteria2','entry',9443,'{}') RETURNING id`, otherNodeID).Scan(&otherInboundID); err != nil {
		t.Fatal("inbound fixture setup failed")
	}

	templateRepo := NewTemplateRepo(resource.db)
	definitionV1 := json.RawMessage(`{"schemaVersion":1,"defaults":{"displayNamePattern":"v1"}}`)
	templateID, err := templateRepo.CreateTemplate(ctx, "P2 template", "mihomo", "first", definitionV1, userID)
	if err != nil {
		t.Fatalf("template creation failed: %v", err)
	}
	draft, err := templateRepo.Get(ctx, templateID)
	if err != nil {
		t.Fatalf("draft lookup failed: %v", err)
	}
	if draft.Revision != 1 || draft.Status != "draft" {
		t.Fatalf("initial draft = revision %d status %q, want 1/draft", draft.Revision, draft.Status)
	}
	draft, err = templateRepo.UpdateDraft(ctx, templateID, draft.Revision, "P2 template", "first", "mihomo", definitionV1)
	if err != nil {
		t.Fatalf("draft update failed: %v", err)
	}
	version1, err := templateRepo.Publish(ctx, templateID, draft.Revision, userID)
	if err != nil {
		t.Fatalf("first template publish failed: %v", err)
	}
	if version1.Version != 1 || version1.PublishedBy == nil || *version1.PublishedBy != userID {
		t.Fatalf("first published version = %+v", version1)
	}
	originalV1, err := templateRepo.GetVersion(ctx, templateID, 1)
	if err != nil {
		t.Fatalf("version 1 lookup failed: %v", err)
	}
	definitionV2 := json.RawMessage(`{"schemaVersion":1,"defaults":{"displayNamePattern":"v2"}}`)
	draft, err = templateRepo.Get(ctx, templateID)
	if err != nil {
		t.Fatalf("published draft lookup failed: %v", err)
	}
	draft, err = templateRepo.UpdateDraft(ctx, templateID, draft.Revision, "P2 template", "second", "mihomo", definitionV2)
	if err != nil {
		t.Fatalf("second draft update failed: %v", err)
	}
	version2, err := templateRepo.Publish(ctx, templateID, draft.Revision, userID)
	if err != nil {
		t.Fatalf("second template publish failed: %v", err)
	}
	if version2.Version != 2 {
		t.Fatalf("second published version = %d, want 2", version2.Version)
	}
	unchangedV1, err := templateRepo.GetVersion(ctx, templateID, 1)
	if err != nil {
		t.Fatalf("immutable version lookup failed: %v", err)
	}
	if string(unchangedV1.Definition) != string(originalV1.Definition) {
		t.Fatalf("version 1 changed from %s to %s", originalV1.Definition, unchangedV1.Definition)
	}
	copyID, err := templateRepo.CopyVersion(ctx, templateID, 1, "P2 template copy", "copied", userID)
	if err != nil {
		t.Fatalf("template copy failed: %v", err)
	}
	copyTemplate, err := templateRepo.Get(ctx, copyID)
	if err != nil {
		t.Fatalf("copied template lookup failed: %v", err)
	}
	if string(copyTemplate.Definition) != string(originalV1.Definition) || copyTemplate.PublishedVersion != nil {
		t.Fatalf("copied template = %+v", copyTemplate)
	}

	subscriptions := NewSubscriptionRepo(resource.db)
	groupIDPtr := groupID
	templateIDPtr := templateID
	subscription := &models.Subscription{
		UserID: userID, Name: "P2 subscription", Token: "p2-template-subscription-token", Format: "mihomo",
		NodeGroupID: &groupIDPtr, TemplateID: &templateIDPtr,
	}
	subscriptionID, err := subscriptions.CreateForUser(ctx, subscription)
	if err != nil {
		t.Fatalf("subscription creation failed: %v", err)
	}
	loaded, err := subscriptions.GetByIDForUser(ctx, userID, subscriptionID)
	if err != nil {
		t.Fatalf("subscription lookup failed: %v", err)
	}
	if loaded.Revision != 1 || loaded.TemplateVersion != nil {
		t.Fatalf("new subscription = %+v", loaded)
	}
	latest, err := templateRepo.ResolveVersion(ctx, loaded.TemplateID, loaded.TemplateVersion)
	if err != nil {
		t.Fatalf("latest template resolution failed: %v", err)
	}
	if latest.Version != 2 {
		t.Fatalf("latest template version = %d, want 2", latest.Version)
	}

	nodePatch := OverridePatch{
		DisplayName: stringPtr("node fallback"),
		Params:      json.RawMessage(`{"sni":"node.example"}`),
	}
	nodeOverride, err := subscriptions.SaveOverrideForUserAtRevision(ctx, userID, subscriptionID, nodeID, 0, nodePatch)
	if err != nil {
		t.Fatalf("node override save failed: %v", err)
	}
	if nodeOverride.Revision != 1 || nodeOverride.SortOrderSet {
		t.Fatalf("node override = %+v, want revision 1 and inherited sort order", nodeOverride)
	}
	loaded, err = subscriptions.GetByIDForUser(ctx, userID, subscriptionID)
	if err != nil {
		t.Fatal("subscription lookup after node override failed")
	}
	if loaded.Revision != 2 {
		t.Fatalf("subscription revision after node override = %d, want 2", loaded.Revision)
	}

	sortOrder := 0
	inboundPatch := OverridePatch{
		DisplayName: stringPtr("inbound override"),
		SortOrder:   &sortOrder,
		Params:      json.RawMessage(`{"sni":"inbound.example"}`),
	}
	inboundOverride, err := subscriptions.SaveInboundOverrideForUserAtRevision(ctx, userID, subscriptionID, nodeID, inboundID, 0, inboundPatch)
	if err != nil {
		t.Fatalf("inbound override save failed: %v", err)
	}
	if inboundOverride.Revision != 1 || !inboundOverride.SortOrderSet || inboundOverride.SortOrder != 0 {
		t.Fatalf("inbound override = %+v", inboundOverride)
	}
	if _, err := subscriptions.SaveInboundOverrideForUserAtRevision(ctx, userID, subscriptionID, nodeID, inboundID, loaded.Revision, inboundPatch); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale subscription revision error = %v, want ErrRevisionConflict", err)
	}
	loaded, err = subscriptions.GetByIDForUser(ctx, userID, subscriptionID)
	if err != nil {
		t.Fatal("subscription lookup after inbound override failed")
	}
	if loaded.Revision != 3 {
		t.Fatalf("subscription revision after inbound override = %d, want 3", loaded.Revision)
	}
	effective, err := subscriptions.EffectiveOverrideRows(ctx, userID, subscriptionID)
	if err != nil {
		t.Fatalf("effective override lookup failed: %v", err)
	}
	if effective[OverrideKey{NodeID: nodeID, InboundID: inboundID}].DisplayName != "inbound override" || effective[OverrideKey{NodeID: nodeID}].DisplayName != "node fallback" {
		t.Fatalf("effective overrides did not retain inbound priority and node fallback: %+v", effective)
	}

	if _, err := subscriptions.SaveOverrideForUserAtRevision(ctx, otherUserID, subscriptionID, nodeID, loaded.Revision, nodePatch); !errors.Is(err, ErrSubscriptionNotFound) {
		t.Fatalf("cross-user override error = %v, want ErrSubscriptionNotFound", err)
	}
	invalidPatch := OverridePatch{Params: json.RawMessage(`{"uuid":"must-not-be-editable"}`)}
	if _, err := subscriptions.SaveOverrideForUserAtRevision(ctx, userID, subscriptionID, nodeID, loaded.Revision, invalidPatch); !errors.Is(err, ErrOverrideFieldForbidden) {
		t.Fatalf("invalid override error = %v, want ErrOverrideFieldForbidden", err)
	}
	if err := subscriptions.DeleteInboundOverrideForUser(ctx, userID, subscriptionID, nodeID, inboundID, inboundOverride.Revision); err != nil {
		t.Fatalf("inbound override delete failed: %v", err)
	}
	loaded, err = subscriptions.GetByIDForUser(ctx, userID, subscriptionID)
	if err != nil {
		t.Fatal("subscription lookup after inbound delete failed")
	}
	if loaded.Revision != 4 {
		t.Fatalf("subscription revision after inbound delete = %d, want 4", loaded.Revision)
	}

	fixedVersion := 1
	updatedSubscription, err := subscriptions.SetTemplateForUser(ctx, userID, subscriptionID, loaded.Revision, &templateIDPtr, &fixedVersion, "mihomo")
	if err != nil {
		t.Fatalf("fixed template selection failed: %v", err)
	}
	if updatedSubscription.TemplateVersion == nil || *updatedSubscription.TemplateVersion != fixedVersion || updatedSubscription.Token != subscription.Token || updatedSubscription.Revision != 5 {
		t.Fatalf("updated subscription = %+v", updatedSubscription)
	}
	if err := templateRepo.Archive(ctx, templateID, versionRevisionForTest(t, templateRepo, templateID)); !errors.Is(err, ErrTemplateInUse) {
		t.Fatalf("in-use template archive error = %v, want ErrTemplateInUse", err)
	}
	copyRevision := copyTemplate.Revision
	if err := templateRepo.Archive(ctx, copyID, copyRevision); err != nil {
		t.Fatalf("unused template archive failed: %v", err)
	}
	archivedCopy, err := templateRepo.Get(ctx, copyID)
	if err != nil {
		t.Fatalf("archived copy lookup failed: %v", err)
	}
	if archivedCopy.Status != "archived" || archivedCopy.ArchivedAt == nil {
		t.Fatalf("archived copy = %+v", archivedCopy)
	}
	if _, err := subscriptions.SaveInboundOverrideForUserAtRevision(ctx, userID, subscriptionID, otherNodeID, otherInboundID, updatedSubscription.Revision, inboundPatch); !errors.Is(err, ErrNodeNotAuthorized) {
		t.Fatalf("unauthorized inbound override error = %v, want ErrNodeNotAuthorized", err)
	}
}

func versionRevisionForTest(t *testing.T, templates *TemplateRepo, id int64) int64 {
	t.Helper()
	template, err := templates.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("template revision lookup failed: %v", err)
	}
	return template.Revision
}
