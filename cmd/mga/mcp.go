package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/kzhuang/mini-golang-agent/internal/config"
	"github.com/kzhuang/mini-golang-agent/internal/mcp"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func approvedFor(cfg *config.Config, cwd string) []string {
	return cfg.ApprovedMCP[cwd]
}

func approve(cfg *config.Config, cwd, name string) error {
	if cfg.ApprovedMCP == nil {
		cfg.ApprovedMCP = map[string][]string{}
	}
	for _, n := range cfg.ApprovedMCP[cwd] {
		if n == name {
			return nil
		}
	}
	cfg.ApprovedMCP[cwd] = append(cfg.ApprovedMCP[cwd], name)
	return cfg.Save()
}

func connectForPrint(ctx context.Context, servers *mcp.Manager) {
	for _, err := range servers.LoadErrors {
		fmt.Fprintln(os.Stderr, "mga: mcp:", err)
	}
	servers.ConnectAll(ctx)
	for _, s := range servers.Snapshot() {
		switch s.Status {
		case mcp.StatusNeedsApproval:
			fmt.Fprintf(os.Stderr, "mga: MCP server %s from .mcp.json is not approved; run: mga mcp approve %s\n", s.Name, s.Name)
		case mcp.StatusFailed:
			fmt.Fprintf(os.Stderr, "mga: MCP server %s failed: %s\n", s.Name, tools.OneLine(s.Err, 200))
		}
	}
}

func mcpCommand(cfg *config.Config, cwd string, args []string) error {
	sub := "list"
	if 0 < len(args) {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list", "ls":
		return mcpList(cfg, cwd)
	case "get":
		if len(args) != 1 {
			return errors.New("usage: mga mcp get <name>")
		}
		return mcpGet(cfg, cwd, args[0])
	case "add":
		return mcpAdd(cfg, cwd, args)
	case "remove", "rm":
		return mcpRemove(cwd, args)
	case "approve":
		if len(args) != 1 {
			return errors.New("usage: mga mcp approve <name>")
		}
		project, err := mcp.ReadServers(mcp.ProjectConfigPath(cwd))
		if err != nil {
			return err
		}
		if _, ok := project[args[0]]; !ok {
			return fmt.Errorf("no MCP server %q in %s", args[0], mcp.ProjectConfigPath(cwd))
		}
		if err := approve(cfg, cwd, args[0]); err != nil {
			return err
		}
		fmt.Printf("Approved %s for %s\n", args[0], cwd)
		return nil
	}
	return fmt.Errorf("unknown mcp command %q (use list, get, add, remove, or approve)", sub)
}

func mcpList(cfg *config.Config, cwd string) error {
	servers := mcp.Load(cwd, approvedFor(cfg, cwd))
	defer servers.Close()
	for _, err := range servers.LoadErrors {
		fmt.Fprintln(os.Stderr, "mga:", err)
	}
	snapshot := servers.Snapshot()
	if len(snapshot) == 0 {
		fmt.Println("No MCP servers. Add one with: mga mcp add <name> -- <command> [args...]")
		return nil
	}
	servers.ConnectAll(context.Background())
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSCOPE\tTRANSPORT\tSTATUS\tTOOLS\tTARGET")
	for _, s := range servers.Snapshot() {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n", s.Name, s.Scope, s.Transport, s.Status, len(s.Tools), tools.OneLine(s.Target, 60))
	}
	w.Flush()
	for _, s := range servers.Snapshot() {
		switch s.Status {
		case mcp.StatusFailed:
			fmt.Printf("\n%s: %s\n", s.Name, tools.OneLine(s.Err, 300))
		case mcp.StatusNeedsApproval:
			fmt.Printf("\n%s comes from .mcp.json and is not approved. Check it, then run: mga mcp approve %s\n", s.Name, s.Name)
		}
	}
	return nil
}

func mcpGet(cfg *config.Config, cwd, name string) error {
	servers := mcp.Load(cwd, approvedFor(cfg, cwd))
	defer servers.Close()
	servers.ConnectAll(context.Background())
	for _, s := range servers.Snapshot() {
		if s.Name != name {
			continue
		}
		fmt.Printf("%s (%s, %s)\n  target: %s\n  status: %s\n", s.Name, s.Scope, s.Transport, s.Target, s.Status)
		if s.Err != "" {
			fmt.Printf("  error:  %s\n", s.Err)
		}
		for _, t := range s.Tools {
			ro := ""
			if t.ReadOnly {
				ro = " [read-only]"
			}
			fmt.Printf("  - %s%s: %s\n", mcp.ExposedName(s.Name, t.Name), ro, tools.OneLine(t.Description, 100))
		}
		return nil
	}
	return fmt.Errorf("no MCP server %q", name)
}

func mcpAdd(cfg *config.Config, cwd string, args []string) error {
	fs := flag.NewFlagSet("mga mcp add", flag.ContinueOnError)
	scope := fs.String("s", mcp.ScopeUser, "scope: user (~/.mga/mcp.json) or project (.mcp.json)")
	fs.StringVar(scope, "scope", mcp.ScopeUser, "same as -s")
	transport := fs.String("t", "", "transport: stdio or http (default: http for a URL, else stdio)")
	fs.StringVar(transport, "transport", "", "same as -t")
	var envs, headers listFlag
	fs.Var(&envs, "e", "environment variable for a stdio server, KEY=VALUE (repeatable)")
	fs.Var(&headers, "H", "HTTP header for an http server, \"Name: value\" (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if 2 < len(rest) && rest[1] == "--" {
		rest = append(rest[:1], rest[2:]...)
	}
	if len(rest) < 2 {
		return errors.New("usage: mga mcp add [-s user|project] [-e KEY=VALUE] <name> -- <command> [args...]\n       mga mcp add -t http [-H \"Name: value\"] <name> <url>")
	}
	name := rest[0]
	if err := mcp.ValidateName(name); err != nil {
		return err
	}
	server := mcp.ServerConfig{}
	if *transport == "" && (strings.HasPrefix(rest[1], "http://") || strings.HasPrefix(rest[1], "https://")) {
		*transport = mcp.TransportHTTP
	}
	switch *transport {
	case "", mcp.TransportStdio:
		server.Command, server.Args = rest[1], rest[2:]
		for _, e := range envs {
			key, value, ok := strings.Cut(e, "=")
			if !ok {
				return fmt.Errorf("bad -e %q; use KEY=VALUE", e)
			}
			if server.Env == nil {
				server.Env = map[string]string{}
			}
			server.Env[key] = value
		}
	case mcp.TransportHTTP:
		server.Type, server.URL = mcp.TransportHTTP, rest[1]
		for _, h := range headers {
			key, value, ok := strings.Cut(h, ":")
			if !ok {
				return fmt.Errorf("bad -H %q; use \"Name: value\"", h)
			}
			if server.Headers == nil {
				server.Headers = map[string]string{}
			}
			server.Headers[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	default:
		return fmt.Errorf("unknown transport %q (use stdio or http)", *transport)
	}
	path, err := mcp.ConfigPath(*scope, cwd)
	if err != nil {
		return err
	}
	if err := mcp.WriteServer(path, name, &server); err != nil {
		return err
	}
	if *scope == mcp.ScopeProject {
		if err := approve(cfg, cwd, name); err != nil {
			return err
		}
	}
	fmt.Printf("Added MCP server %s to %s. Check it with: mga mcp get %s\n", name, path, name)
	return nil
}

func mcpRemove(cwd string, args []string) error {
	fs := flag.NewFlagSet("mga mcp remove", flag.ContinueOnError)
	scope := fs.String("s", "", "scope: user or project (default: wherever the server is)")
	fs.StringVar(scope, "scope", "", "same as -s")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: mga mcp remove [-s user|project] <name>")
	}
	name := fs.Arg(0)
	scopes := []string{mcp.ScopeProject, mcp.ScopeUser}
	if *scope != "" {
		scopes = []string{*scope}
	}
	for _, sc := range scopes {
		path, err := mcp.ConfigPath(sc, cwd)
		if err != nil {
			return err
		}
		if servers, _ := mcp.ReadServers(path); servers != nil {
			if _, ok := servers[name]; ok {
				if err := mcp.WriteServer(path, name, nil); err != nil {
					return err
				}
				fmt.Printf("Removed MCP server %s from %s\n", name, path)
				return nil
			}
		}
	}
	return fmt.Errorf("no MCP server %q found", name)
}
