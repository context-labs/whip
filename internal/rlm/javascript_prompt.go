package rlm

import (
	"regexp"
	"strings"
)

var keywordArgument = regexp.MustCompile(`\b([A-Za-z_][A-Za-z_0-9]*)=`)

// BuildPromptForEngine keeps shared authority and lifecycle policy in one source
// while replacing language syntax and persistence guarantees for the driver.
func BuildPromptForEngine(engineID, workingDirectory string, history *ContextHandle) string {
	if engineID != EngineQuickJS {
		return BuildPrompt(workingDirectory, history)
	}
	prompt := BuildPrompt(workingDirectory, history)
	lines := strings.Split(prompt, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "You are an expert coding agent."):
			lines[i] = "You are an expert coding agent. Your only tool is rlm_exec, a bounded JavaScript (QuickJS) runtime. Use short cells, await host-module Promises, and retain working variables. Your ordinary assistant response completes the current turn."
		case line == "Available Starlark modules:":
			lines[i] = "Available host modules (every operation returns a Promise and takes one object argument):"
		case strings.HasPrefix(line, "- json.encode"):
			lines[i] = "- Local helpers: print(value, ...), console.log/info/warn/error; native Math, Date, JSON and BigInt. json.encode(value) and json.decode(text) provide the lossless host-number codec; ordinary JSON.stringify does not support BigInt. These helpers do not make host requests. Date uses UTC; Math.random is available. Decode complete JSON, not incomplete handle-read chunks."
		case strings.HasPrefix(line, "- Host module operations accept"):
			lines[i] = "- Use await files.read({path: \"README.md\"}), await state.private_set({key: \"progress\", value: ...}), or await Promise.all([files.read({path: \"a\"}), files.read({path: \"b\"})]). Zero-argument operations accept no argument or {}. Default limits: 32 MiB guest heap, 64 MiB WASM memory, 256 MiB worker RSS, 1,024 host requests per cell, 16 outstanding host calls, 100,000 queued jobs drained per cell, 30 seconds guest compute, 10 minutes whole-cell watchdog, 64 KiB output, and 40 MiB checkpoint. Use batches for larger fan-out. Local helpers accept positional arguments."
		case strings.HasPrefix(line, "- Starlark is not Python:"):
			lines[i] = "- This is QuickJS, not Node.js: no process, require, npm, filesystem imports, network APIs, timers, or dynamic module loading. Proxy construction is disabled so host payload validation never invokes proxy traps. Native global async evaluation preserves top-level let/const, functions, closures, cycles and class instances between cells. Redeclaring a lexical let/const is an error; prefer a fresh name, var, or mutable containers. A throwing const initializer can leave an uninitialized lexical binding."
		case strings.HasPrefix(line, "- Scratch checkpoints retain"):
			lines[i] = "- Scratch checkpoints capture the complete JavaScript heap only after the top-level evaluation, every owned host call, and every queued job settles. Forgotten awaits are drained; unhandled rejections and stalled Promises fail the cell. Ordinary errors retain partial mutations. No detached work inherits the next cell's authority. Cancellation or worker loss discards live state and restores the last committed image; completed external effects remain. Check checkpoint warnings and never repeat effects to repair a checkpoint. Important durable work belongs in state, artifacts, messages, and children."
		case strings.HasPrefix(line, "- State values are JSON-compatible"):
			lines[i] = "- Host/state/MCP/mail payloads are passive JSON trees: null, booleans, strings, finite safe Number values, BigInt integers, arrays and plain objects. Undefined, functions, cycles, accessors, symbols and unsafe integer Numbers are rejected. Construct large integers from strings, e.g. BigInt(\"9007199254740993\"). Incoming large integers decode as BigInt; decimal/exponent tokens decode as immutable exact-number wrappers that round-trip losslessly through json.encode and host calls. Arithmetic or Number(wrapper) explicitly converts them to Number. Negative zero is retained as -0.0. Native heap values may be richer than payloads; user-visible results use tagged previews for BigInt and exact numbers. Set/append/CAS require explicit value; null stores JSON null. CAS uses the returned version; on conflict reread before deciding what to write."
		}
	}
	return strings.ReplaceAll(javascriptExamples(strings.Join(lines, "\n")), "include_grants=True", "include_grants: true")
}

func IdentityBlockForEngine(engineID string, identity Identity) string {
	text := IdentityBlock(identity)
	if engineID == EngineQuickJS {
		return javascriptExamples(text)
	}
	return text
}

// Only rewrite actual host-call examples, leaving policy prose and local APIs
// unchanged. This also covers shared mailbox, citation and identity guidance.
func javascriptExamples(text string) string {
	var result strings.Builder
	for i := 0; i < len(text); {
		matched := false
		for module, operations := range moduleRegistry {
			for _, operation := range operations {
				prefix := module + "." + operation + "("
				if !strings.HasPrefix(text[i:], prefix) {
					continue
				}
				start, end, depth := i+len(prefix), i+len(prefix), 1
				var quote byte
				for end < len(text) && depth > 0 {
					c := text[end]
					if quote != 0 {
						if c == '\\' {
							end++
						} else if c == quote {
							quote = 0
						}
					} else {
						switch c {
						case '\'', '"':
							quote = c
						case '(':
							depth++
						case ')':
							depth--
						}
					}
					end++
				}
				if depth != 0 {
					continue
				}
				args := text[start : end-1]
				// Explicit JavaScript examples were already selected above.
				if strings.HasPrefix(strings.TrimSpace(args), "{") {
					result.WriteString(text[i:end])
					i = end
					matched = true
					break
				}
				args = keywordArgument.ReplaceAllString(args, "$1: ")
				args = strings.NewReplacer("True", "true", "False", "false", "None", "null").Replace(args)
				if args == "id" {
					args = "id: id"
				}
				result.WriteString("await " + prefix + "{" + args + "})")
				i = end
				matched = true
				break
			}
			if matched {
				break
			}
		}
		if !matched {
			result.WriteByte(text[i])
			i++
		}
	}
	return result.String()
}
