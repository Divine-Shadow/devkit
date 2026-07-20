package productadapter

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	ProductProject = "dev-all"
	ProductRepo    = "ouroboros-ide"
)

type CommandKind string

const (
	CommandPrepare CommandKind = "prepare"
	CommandExec    CommandKind = "exec"
)

// Command is the single parsed Product adapter request. Product arguments are
// not parsed again by the native command implementation.
type Command struct {
	Kind  CommandKind
	Count int
	Index int
	Child []string
}

func InvocationSelectsProduct(project, command string, args []string, defaultRepo, origin string, paths ...string) bool {
	if strings.TrimSpace(project) == ProductProject ||
		strings.TrimSpace(defaultRepo) == ProductRepo ||
		isProductOrigin(origin) {
		return true
	}
	if strings.TrimSpace(command) == "native" {
		for index := 0; index < len(args); index++ {
			if strings.TrimSpace(args[index]) != "--repo" || index+1 >= len(args) {
				continue
			}
			if strings.TrimSpace(args[index+1]) == ProductRepo {
				return true
			}
		}
	}
	for _, argument := range args {
		argument = strings.TrimSpace(argument)
		if argument == ProductRepo || pathSelectsProduct(argument) {
			return true
		}
	}
	for _, path := range paths {
		if pathSelectsProduct(path) {
			return true
		}
	}
	return false
}

func pathSelectsProduct(path string) bool {
	normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(path)), "\\", "/")
	for _, component := range strings.Split(normalized, "/") {
		if component == ProductRepo {
			return true
		}
	}
	return false
}

func isProductOrigin(origin string) bool {
	origin = strings.ToLower(strings.TrimSpace(origin))
	origin = strings.TrimSuffix(origin, "/")
	origin = strings.TrimSuffix(origin, ".git")
	return strings.HasSuffix(origin, "/"+ProductRepo) ||
		strings.HasSuffix(origin, ":"+ProductRepo)
}

// Parse requires the exact dedicated adapter grammar. Product identity is
// implicit. Duplicates, aliases, equals-form flags, omitted values, and
// trailing authority overrides are rejected by construction.
func Parse(args []string) (*Command, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("Product adapter permits only prepare or exec")
	}
	kind := CommandKind(args[0])
	switch kind {
	case CommandPrepare:
		return parsePrepare(args[1:])
	case CommandExec:
		return parseExecution(kind, args[1:])
	default:
		return nil, fmt.Errorf("Product adapter refuses %q", args[0])
	}
}

func parsePrepare(args []string) (*Command, error) {
	if len(args) != 4 ||
		args[0] != "--count" ||
		args[2] != "--index" {
		return nil, fmt.Errorf("Product construction requires exactly: product-adapter prepare --count N --index N")
	}
	return parseIdentity(CommandPrepare, args[1], args[3])
}

func parseExecution(kind CommandKind, args []string) (*Command, error) {
	if len(args) < 5 ||
		args[0] != "--count" ||
		args[2] != "--index" ||
		args[4] != "--" {
		return nil, fmt.Errorf("Product execution requires exactly: product-adapter %s --count N --index N -- COMMAND", kind)
	}
	command, err := parseIdentity(kind, args[1], args[3])
	if err != nil {
		return nil, err
	}
	command.Child = append([]string(nil), args[5:]...)
	if len(command.Child) == 0 {
		return nil, fmt.Errorf("Product execution requires a command after --")
	}
	return command, nil
}

func parseIdentity(kind CommandKind, rawCount, rawIndex string) (*Command, error) {
	count, err := parseCanonicalPositiveDecimal(rawCount)
	if err != nil {
		return nil, fmt.Errorf("Product adapter --count must be a byte-canonical positive decimal")
	}
	index, err := parseCanonicalPositiveDecimal(rawIndex)
	if err != nil || index > count {
		return nil, fmt.Errorf("Product adapter --index must be between 1 and --count")
	}
	return &Command{
		Kind:  kind,
		Count: count,
		Index: index,
	}, nil
}

func parseCanonicalPositiveDecimal(raw string) (int, error) {
	if raw == "" || raw[0] < '1' || raw[0] > '9' {
		return 0, fmt.Errorf("not canonical")
	}
	for _, value := range []byte(raw[1:]) {
		if value < '0' || value > '9' {
			return 0, fmt.Errorf("not canonical")
		}
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || strconv.Itoa(value) != raw {
		return 0, fmt.Errorf("not canonical")
	}
	return value, nil
}

// ParseCanonicalIdentity is shared by the subordinate helper so its public
// numeric grammar is byte-identical to the adapter grammar.
func ParseCanonicalIdentity(rawCount, rawIndex string) (int, int, error) {
	command, err := parseIdentity(CommandPrepare, rawCount, rawIndex)
	if err != nil {
		return 0, 0, err
	}
	return command.Count, command.Index, nil
}
