package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"vpn-probe/internal/config"
	"vpn-probe/internal/model"
	"vpn-probe/internal/monitor"
	"vpn-probe/internal/publish"
	"vpn-probe/internal/substore"
)

func main() {
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("LoadConfig: %v", err)
	}

	cfg.SubStore.FallbackURL = cfg.SubStore.URL
	client := substore.NewClient(&cfg.SubStore)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	fmt.Println("1. Initializing Scheduler & Orchestrator to collect real atoms...")
	sched := monitor.NewScheduler(cfg, nil, nil, client)
	orc := monitor.NewOrchestrator(sched)
	atoms := orc.CollectAtoms()
	fmt.Printf("   Collected %d atoms:\n", len(atoms))
	for _, a := range atoms {
		fmt.Printf("     [%s] %s (Country: %s, ChinaExit: %v)\n", a.Kind, a.Name, a.Country, a.ChinaExit)
	}

	fmt.Println("2. Loading custom groups for ashan, Q and business...")
	groupsAshan, err := monitor.LoadCustomGroups("ashan")
	if err != nil {
		groupsAshan = model.DefaultCustomGroups("ashan")
	}
	groupsQ, err := monitor.LoadCustomGroups("q")
	if err != nil {
		groupsQ = model.DefaultCustomGroups("q")
	}
	groupsBusiness, err := monitor.LoadCustomGroups("business")
	if err != nil {
		groupsBusiness = model.DefaultCustomGroups("business")
	}

	for _, g := range groupsAshan {
		matched := monitor.MatchAtoms(atoms, g.Filter)
		fmt.Printf("   Ashan Group [%s] matched %d: ", g.DisplayName, len(matched))
		for _, m := range matched {
			fmt.Printf("%s, ", m.Name)
		}
		fmt.Println()
	}

	fmt.Println("3. Compiling differentiated scripts with live atoms...")
	scriptAshan, err := publish.CompileScript(groupsAshan, atoms, "ashan")
	if err != nil {
		log.Fatalf("CompileScript ashan: %v", err)
	}
	scriptQ, err := publish.CompileScript(groupsQ, atoms, "q")
	if err != nil {
		log.Fatalf("CompileScript q: %v", err)
	}
	scriptBusiness, err := publish.CompileScript(groupsBusiness, atoms, "business")
	if err != nil {
		log.Fatalf("CompileScript business: %v", err)
	}

	for _, line := range strings.Split(scriptAshan, "\n") {
		if strings.HasPrefix(line, "const vpnUserGroups = ") {
			fmt.Println("EMBEDDED GROUPS:", line[:min(len(line), 500)])
			break
		}
	}

	fmt.Printf("   ashan script length: %d, Q script length: %d, business script length: %d\n", len(scriptAshan), len(scriptQ), len(scriptBusiness))

	expectedAshan := []string{}
	for _, g := range groupsAshan {
		if g.Enabled {
			expectedAshan = append(expectedAshan, g.DisplayName)
		}
	}
	expectedQ := []string{}
	for _, g := range groupsQ {
		if g.Enabled {
			expectedQ = append(expectedQ, g.DisplayName)
		}
	}
	expectedBusiness := []string{}
	for _, g := range groupsBusiness {
		if g.Enabled {
			expectedBusiness = append(expectedBusiness, g.DisplayName)
		}
	}

	fmt.Println("4. Publishing new operators to Sub-Store files (ashan, Q and business)...")
	pub := publish.Publisher{Client: client}
	err = pub.ApplyDifferentiated(
		ctx,
		scriptAshan,
		scriptQ,
		scriptBusiness,
		func(fileName string, yamlBytes []byte) error {
			if strings.EqualFold(fileName, "q") {
				return publish.ValidateYAMLWithProfile(yamlBytes, expectedQ, nil, "q")
			}
			if strings.EqualFold(fileName, "business") {
				return publish.ValidateYAMLWithProfile(yamlBytes, expectedBusiness, nil, "business")
			}
			return publish.ValidateYAMLWithProfile(yamlBytes, expectedAshan, nil, "ashan")
		},
		func(step string) {
			fmt.Printf("   [Progress] %s\n", step)
		},
	)
	if err != nil {
		log.Fatalf("ApplyDifferentiated: %v", err)
	}
	fmt.Println("   Successfully applied differentiated operators to Sub-Store files!")

	// 5. Purge transit nodes from all residential subscriptions in Sub-Store
	fmt.Println("5. Purging transit nodes from residential subscriptions in Sub-Store...")
	transitNames := []string{
		"US-LAX-ACCK-4837-Reality",
		"JP-Tokyo-IIJ-Reality",
		"JP--ACCK-SB",
	}

	cleanSub := func(subName string) {
		content, err := client.GetSubscriptionContent(ctx, subName)
		if err != nil {
			fmt.Printf("   [%s] GetSubscriptionContent error: %v\n", subName, err)
			return
		}
		lines := strings.Split(content, "\n")
		var cleanLines []string
		purgedCount := 0
		for _, l := range lines {
			trimmed := strings.TrimSpace(l)
			if trimmed == "" {
				continue
			}
			// Check if this line is a direct transit node (has #TransitName and doesn't start with dialerProxy or 链式)
			isTransit := false
			for _, tn := range transitNames {
				if strings.Contains(trimmed, "#"+tn) && !strings.Contains(trimmed, "dialerProxy=") && !strings.Contains(trimmed, "链式") {
					isTransit = true
					break
				}
			}
			if isTransit {
				purgedCount++
				continue
			}
			cleanLines = append(cleanLines, trimmed)
		}
		if purgedCount > 0 {
			fmt.Printf("   [%s] Purging %d duplicate transit nodes...\n", subName, purgedCount)
			if err := client.UpdateSubscriptionNodes(ctx, subName, cleanLines); err != nil {
				fmt.Printf("   [%s] UpdateSubscriptionNodes error: %v\n", subName, err)
			} else {
				fmt.Printf("   [%s] Successfully cleaned! (%d nodes remaining)\n", subName, len(cleanLines))
			}
		} else {
			fmt.Printf("   [%s] Already clean (0 transit nodes found, %d nodes total).\n", subName, len(cleanLines))
		}
	}

	cleanSub("ashan-us-resi")
	cleanSub("ashan-jp-resi")
	cleanSub("ashan-tw-resi")
	cleanSub("ashan-other")
	cleanSub("ashan-residential")

	fmt.Println("All direct publish and purge operations completed successfully!")
}
