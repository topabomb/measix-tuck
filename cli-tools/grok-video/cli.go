package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	cli "github.com/measix/measix-tuck/cli-tools/common"
)

type commonOptions struct {
	base, keyEnv, keyFile string
	timeout               time.Duration
}

type waitOptions struct {
	wait, overwrite bool
	output          string
	interval, limit time.Duration
	maxBytes        int64
}

const rootHelp = `grok-video — independent xAI video creation CLI

Usage:
  grok-video COMMAND [OPTIONS] [REQUEST_ID]
  grok-video help [COMMAND]
  grok-video --version

Commands:
  generate  Create a video from text, images, voices or pinned frames.
  edit      Change an existing video (grok-imagine-video).
  extend    Append a new segment to an existing video (grok-imagine-video).
  status    Fetch a request's current result once; does not wait/download.
  wait      Poll an existing request until it finishes; optionally download.
  download  Download a completed request; --wait also permits pending jobs.
  models    List the provider's model catalog, filtered by a name prefix.
  docs      Print the complete embedded Markdown manual, offline.
  version   Print the executable version.
  help      Show this help or complete help for a command.

Connection and authentication:
  Put options AFTER the command (before/after REQUEST_ID is supported).
  --base-url defaults to GROK_BASE_URL, then https://api.x.ai/v1.
  Set GROK_API_KEY (fallback: XAI_API_KEY), or use --api-key-env NAME /
  --api-key-file PATH. Keys are never read from Tuck configuration.
  Compatible services use an explicit base URL, for example:
    https://video-api.example.com/v1

Output and lifecycle:
  stdout: one JSON result (except help/version/docs); stderr: progress/errors.
  Create commands submit once and return request_id by default.
  --wait polls; --output FILE implies --wait and downloads the MP4.
  Ctrl+C/timeout stops local work, NOT the remote generation. Keep the ID.
  There are no automatic retries, saved credentials, or local task database.

Exit codes:
  0 success (status reports remote failure as JSON with exit 0)
  1 local I/O failure         2 invalid arguments/configuration
  3 HTTP/transport/protocol  4 remote job failed/expired/filtered/not ready
  5 wait deadline exceeded  130 interrupted (Ctrl+C)

Examples:
  grok-video generate --prompt "A cat turns toward the camera" --output cat.mp4
  grok-video generate --base-url https://video-api.example.com/v1 --model grok-imagine-video --prompt-file scene.txt
  grok-video wait REQUEST_ID --output scene.mp4
  grok-video help generate
`

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || (args[0] == "help" && len(args) == 1) {
		_, err := io.WriteString(stdout, rootHelp)
		return err
	}
	if args[0] == "help" {
		if len(args) != 2 {
			return cli.Errorf(2, "usage: grok-video help [COMMAND]")
		}
		args = []string{args[1], "--help"}
	}
	if args[0] == "docs" {
		if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
			_, err := io.WriteString(stdout, docsHelp)
			return err
		}
		if len(args) != 1 {
			return cli.Errorf(2, "docs takes no options; redirect stdout to save Markdown")
		}
		return writeDocs(stdout)
	}
	if args[0] == "version" || args[0] == "--version" {
		if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
			_, err := io.WriteString(stdout, "Usage: grok-video version\nPrint the executable version; no credentials or network required. Exit 0.\n")
			return err
		}
		if len(args) != 1 {
			return cli.Errorf(2, "version takes no arguments")
		}
		_, err := fmt.Fprintln(stdout, "grok-video", version)
		return err
	}
	command := args[0]
	create := command == "generate" || command == "edit" || command == "extend"
	if !create && command != "status" && command != "wait" && command != "download" && command != "models" {
		return cli.Errorf(2, "unknown command %q; run grok-video --help", command)
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var help bool
	fs.BoolVar(&help, "help", false, "Show complete command help; no credentials/network required.")
	fs.BoolVar(&help, "h", false, "Alias for --help.")
	var common commonOptions
	base := getenv("GROK_BASE_URL")
	if base == "" {
		base = "https://api.x.ai/v1"
	}
	fs.StringVar(&common.base, "base-url", base, "API `URL` prefix INCLUDING /v1; env GROK_BASE_URL or https://api.x.ai/v1.")
	fs.StringVar(&common.keyEnv, "api-key-env", "GROK_API_KEY", "Read key from environment variable `NAME`; default falls back to XAI_API_KEY.")
	fs.StringVar(&common.keyFile, "api-key-file", "", "Read key from UTF-8 `PATH`; mutually exclusive with an explicit --api-key-env.")
	fs.DurationVar(&common.timeout, "timeout", 2*time.Minute, "Per HTTP request/download `DURATION`, e.g. 30s, 2m; must be positive.")
	var o createOptions
	var w waitOptions
	var dryRun bool
	var prefix string
	if create {
		defaultModel := "grok-imagine-video"
		if command == "generate" {
			defaultModel = "grok-imagine-video-1.5"
		}
		fs.StringVar(&o.model, "model", defaultModel, "Exact upstream model `ID`; unknown aliases are forwarded without renaming.")
		fs.StringVar(&o.prompt, "prompt", "", "Creative `TEXT`; required for text generation/edit/extend; conflicts with --prompt-file.")
		fs.StringVar(&o.promptFile, "prompt-file", "", "Read UTF-8 prompt from `PATH` or - for stdin (maximum 1 MiB).")
		fs.StringVar(&o.uploadURL, "upload-url", "", "Optional signed HTTP(S) `URL` for upstream PUT delivery of the MP4.")
		fs.StringVar(&o.storeFilename, "store-filename", "", "Optional xAI Files `NAME`; enables remote storage, NOT local output.")
		fs.IntVar(&o.storeExpires, "store-expires", 0, "Stored file TTL in `SECONDS`, 3600..2592000; 0 means omit/no auto-expiry.")
		fs.BoolVar(&o.storePublic, "store-public", false, "Request a public stored-file URL; requires --store-filename.")
		fs.IntVar(&o.publicExpires, "store-public-expires", 0, "Public URL TTL in `SECONDS`; requires --store-public, cannot exceed file TTL; 0 inherits.")
		fs.BoolVar(&dryRun, "dry-run", false, "Validate and print {method,path,body}; reads inputs but sends no request and needs no key.")
		if command != "extend" {
			fs.StringVar(&o.user, "user", "", "Optional end-user `ID` sent to the provider (not an API key).")
		}
		if command == "generate" {
			fs.IntVar(&o.duration, "duration", 8, "Clip length in integer `SECONDS`, 1..15; classic reference mode at most 10.")
			fs.StringVar(&o.aspectRatio, "aspect-ratio", "", "`RATIO`: 1:1,16:9,9:16,4:3,3:4,3:2,2:3,21:9,5:2; omit for provider default 16:9.")
			fs.StringVar(&o.resolution, "resolution", "", "`SIZE`: 480p,720p,1080p; omit for provider default 480p; see model constraints below.")
			fs.StringVar(&o.audio, "audio", "auto", "`MODE`: auto omits field (provider normally adds audio); on/off sends generate_audio true/false.")
			fs.StringVar(&o.image, "image", "", "First-frame image `SOURCE`: local file, HTTP(S) URL, data URL or file-id:ID.")
			fs.Var(&o.images, "reference-image", "Repeatable reference image `SOURCE`; same formats as --image; array order is preserved.")
			fs.Var(&o.voices, "reference-voice", "Repeatable preset/custom voice `ID` (e.g. eve); 1.5 only, total voices/clips at most 3.")
			fs.Var(&o.audios, "reference-audio", "Repeatable audio `SOURCE`: local file/URL/data URL, at most 15s upstream; special account access required.")
			fs.StringVar(&o.lastFrame, "last-frame", "", "Pinned final-frame image `SOURCE`; 1.5 only; same formats as --image.")
			fs.Var(&o.keyframes, "keyframe", "Repeatable `SECONDS=SOURCE` interior image anchor; 1.5 only, maximum 4 distinct 1/3-second slots.")
		} else {
			fs.StringVar(&o.video, "video", "", "Required MP4 `SOURCE`: local file, HTTP(S) URL, data URL or file-id:ID.")
			if command == "extend" {
				fs.IntVar(&o.duration, "duration", 6, "NEW segment length in `SECONDS`, 2..10 (not total output duration).")
			}
		}
	}
	if create || command == "wait" || command == "download" {
		if command != "wait" {
			fs.BoolVar(&w.wait, "wait", false, "Poll until completion; Ctrl+C only stops this client. --output implies this for create commands.")
		} else {
			w.wait = true
		}
		fs.DurationVar(&w.interval, "poll-interval", 5*time.Second, "Delay `DURATION` between pending-result queries; must be positive.")
		fs.DurationVar(&w.limit, "wait-timeout", 10*time.Minute, "Total polling `DURATION`, starting after submission; excludes later download.")
		fs.StringVar(&w.output, "output", "", "Local MP4 `PATH`; parent must exist; required for download; create commands also wait.")
		fs.BoolVar(&w.overwrite, "overwrite", false, "Explicitly replace an existing regular output file; requires --output.")
		fs.Int64Var(&w.maxBytes, "max-download-bytes", 512<<20, "Maximum downloaded `BYTES`; aborts oversized content without publishing a partial file.")
	}
	if command == "models" {
		fs.StringVar(&prefix, "prefix", "grok-imagine-video", "Model ID `PREFIX` filter; use --prefix= to list the entire /models catalog.")
	}
	ordered, err := cli.OrderFlags(fs, args[1:])
	if err != nil {
		return cli.Errorf(2, "%v; run grok-video help %s", err, command)
	}
	if err := fs.Parse(ordered); err != nil {
		return cli.Errorf(2, "%v; run grok-video help %s", err, command)
	}
	if help {
		printCommandHelp(stdout, fs, command)
		return nil
	}
	needID := command == "status" || command == "wait" || command == "download"
	if (needID && fs.NArg() != 1) || (!needID && fs.NArg() != 0) {
		return cli.Errorf(2, "wrong positional arguments; run grok-video help %s", command)
	}
	id := ""
	if needID {
		id = fs.Arg(0)
		if err := validateID(id); err != nil {
			return err
		}
	}
	if common.timeout <= 0 {
		return cli.Errorf(2, "--timeout must be positive")
	}
	if create || command == "wait" || command == "download" {
		if w.interval <= 0 || w.limit <= 0 || w.maxBytes < 12 || w.maxBytes >= 1<<62 {
			return cli.Errorf(2, "poll/wait durations must be positive and --max-download-bytes must be 12..2^62-1")
		}
		if w.overwrite && w.output == "" {
			return cli.Errorf(2, "--overwrite requires --output")
		}
		if command == "download" && w.output == "" {
			return cli.Errorf(2, "download requires --output FILE")
		}
		if w.output != "" {
			if err := checkOutput(w.output, w.overwrite); err != nil {
				return err
			}
			if create {
				w.wait = true
			}
		}
	}
	var payload map[string]any
	endpoint := map[string]string{"generate": "/videos/generations", "edit": "/videos/edits", "extend": "/videos/extensions"}[command]
	if create {
		payload, err = buildRequest(command, o, stdin)
		if err != nil {
			return cli.Errorf(2, "%v", err)
		}
		if dryRun {
			if _, err := newClient(common.base, "", common.timeout); err != nil {
				return err
			}
			return cli.WriteJSON(stdout, map[string]any{"method": "POST", "path": endpoint, "body": payload})
		}
	}
	explicitEnv := false
	fs.Visit(func(f *flag.Flag) { explicitEnv = explicitEnv || f.Name == "api-key-env" })
	key, err := readKey(common, explicitEnv, getenv)
	if err != nil {
		return err
	}
	c, err := newClient(common.base, key, common.timeout)
	if err != nil {
		return err
	}
	stdout = redactingWriter{stdout, key}
	stderr = redactingWriter{stderr, key}
	var result map[string]any
	if create {
		result, err = c.request(ctx, "POST", endpoint, payload)
		if err != nil {
			return err
		}
		id, _ = result["request_id"].(string)
		if validateID(id) != nil {
			return cli.Errorf(3, "creation returned no valid request_id; outcome may be unknown; do not blindly resubmit")
		}
		fmt.Fprintf(stderr, "submitted %s; keep this ID to resume status/wait/download\n", id)
	} else if command == "models" {
		result, err = c.request(ctx, "GET", "/models", nil)
		if err == nil {
			list, ok := result["data"].([]any)
			if !ok {
				return cli.Errorf(3, "model catalog is missing data array")
			}
			filtered := make([]any, 0, len(list))
			for _, item := range list {
				m, ok := item.(map[string]any)
				if !ok {
					return cli.Errorf(3, "model catalog contains a non-object entry")
				}
				modelID, ok := m["id"].(string)
				if !ok {
					return cli.Errorf(3, "model catalog entry has no id")
				}
				if strings.HasPrefix(modelID, prefix) {
					filtered = append(filtered, item)
				}
			}
			result["data"] = filtered
		}
	} else if !w.wait {
		result, err = c.status(ctx, id)
	}
	if err != nil {
		return err
	}
	if w.wait {
		result, err = c.wait(ctx, id, w.interval, w.limit, stderr)
		if err != nil {
			if result != nil {
				if writeErr := cli.WriteJSON(stdout, result); writeErr != nil {
					return writeErr
				}
			}
			return err
		}
	}
	if w.output != "" {
		if err := c.download(ctx, result, w.output, w.overwrite, w.maxBytes); err != nil {
			return err
		}
	}
	return cli.WriteJSON(stdout, result)
}

func readKey(o commonOptions, explicitEnv bool, getenv func(string) string) (string, error) {
	if o.keyFile != "" && explicitEnv {
		return "", cli.Errorf(2, "--api-key-file and --api-key-env are mutually exclusive")
	}
	key := ""
	if o.keyFile != "" {
		f, err := os.Open(o.keyFile)
		if err != nil {
			return "", fmt.Errorf("read API key file: %w", err)
		}
		defer f.Close()
		b, err := readBounded(f, 64<<10)
		if err != nil {
			return "", cli.Errorf(2, "key file must be at most 64 KiB")
		}
		key = strings.TrimPrefix(string(b), "\ufeff")
	} else {
		if o.keyEnv == "" {
			return "", cli.Errorf(2, "--api-key-env cannot be empty")
		}
		key = getenv(o.keyEnv)
		if key == "" && !explicitEnv {
			key = getenv("XAI_API_KEY")
		}
	}
	key = strings.TrimSpace(key)
	if key == "" || strings.ContainsAny(key, "\r\n\t ") {
		return "", cli.Errorf(2, "missing/invalid API key; set GROK_API_KEY or XAI_API_KEY, or use --api-key-file/--api-key-env")
	}
	return key, nil
}

type redactingWriter struct {
	w   io.Writer
	key string
}

func (r redactingWriter) Write(p []byte) (int, error) {
	_, err := io.WriteString(r.w, strings.ReplaceAll(string(p), r.key, "[REDACTED]"))
	return len(p), err
}

func printCommandHelp(w io.Writer, fs *flag.FlagSet, command string) {
	usage := "grok-video " + command + " [OPTIONS]"
	if command == "status" || command == "wait" || command == "download" {
		usage += " REQUEST_ID"
	}
	fmt.Fprintf(w, "Usage: %s\n\n%s\n\nOptions:\n", usage, commandDetails[command])
	cli.PrintFlags(w, fs)
	fmt.Fprint(w, `
Common contract:
  API key: --api-key-file OR --api-key-env; default GROK_API_KEY falls back
  to XAI_API_KEY only when --api-key-env was not explicitly provided.
  --base-url includes /v1. Options follow the command, before/after the ID.
  stdout is JSON; stderr is progress/errors. No automatic request retries.
  Timeouts use Go durations (500ms, 30s, 2m). --timeout applies per HTTP call.
  Exit: 0 success, 1 local I/O, 2 arguments/config, 3 HTTP/protocol,
        4 job failure/not ready, 5 wait timeout, 130 Ctrl+C.
  Ctrl+C/timeout does not cancel the upstream job. Resume using its ID and
  the same --base-url/credential settings. Never blindly repeat a POST
  after a lost response: it may already have created a billable job.
`)
}

var commandDetails = map[string]string{
	"generate": `Create one video: POST /videos/generations. Default is submit-only.
--wait polls; --output implies waiting and downloads one MP4.
--dry-run validates/prints the exact body without network or credentials.

Sources: local paths are encoded as data URLs; remote HTTP(S)/data URLs
are passed through; file-id:ID references a previously uploaded xAI file.
Images: JPEG/PNG/WebP. Each local/data input <=32 MiB; request <=128 MiB.
Files are resolved relative to the current directory, not the executable.

Known model constraints:
  grok-imagine-video: 1..15s; <=720p; references <=7 images/10s;
    cannot combine --image with references; no voices/last/keyframes.
  grok-imagine-video-1.5 (including -preview): text/image <=1080p;
    references/pins <=720p, <=14 reference images, <=3 voices/audio clips,
    <=4 keyframes, <=15s. --last-frame and --keyframe are 1.5 features.
  grok-imagine-video-1.5-lite: text/image only, no references/pinned frames.
Unknown model IDs pass through unchanged; the provider validates abilities.
Image-to-video follows the source aspect ratio per current xAI guidance.
--prompt can be omitted for --image/--last-frame/--keyframe; supply it for
text and reference-only requests. Upstream model rules still apply.
Reference image ordering maps to <IMAGE_0>, <IMAGE_1>, ...; a pinned first
image takes <IMAGE_0>. Reference voices precede reference audio clips,
then map to <AUDIO_0>, <AUDIO_1>, ... in that order.
Keyframe times must be strictly inside (0,duration), with distinct rounded
1/3-second slots. Input audio duration/permissions are checked upstream.

Examples:
  grok-video generate --prompt-file scene.txt --duration 6 --output clip.mp4
  grok-video generate --image first.png --last-frame last.png --keyframe 2=middle.png --duration 4 --output transition.mp4
  grok-video generate --reference-image person.png --reference-voice eve --prompt "<IMAGE_0> speaks using <AUDIO_0>" --wait
  grok-video generate --model grok-imagine-video --prompt "A quiet lake" --dry-run`,
	"edit": `Edit a source clip: POST /videos/edits. Default is submit-only.
Requires --video and --prompt/--prompt-file; --output implies --wait.
Model: grok-imagine-video. Source MP4 <=8.7s (validated upstream).
Keeps source duration/aspect; resolution is capped at 720p. Duration,
aspect ratio and resolution overrides are not supported for editing.
Video SOURCE: local MP4 (<=32 MiB), HTTP(S)/base64 data URL or file-id:ID.

Example:
  grok-video edit --video source.mp4 --prompt "Change the ball to blue; keep the camera fixed" --output edited.mp4`,
	"extend": `Append a segment: POST /videos/extensions. Default is submit-only.
Requires --video and --prompt/--prompt-file; --output implies --wait.
Model: grok-imagine-video. Source MP4 2..15s (validated upstream).
--duration is NEW content, 2..10s, default 6; output includes the original.
Keeps source aspect/resolution, capped at 720p. No aspect/resolution flags.
Video SOURCE: local MP4 (<=32 MiB), HTTP(S)/base64 data URL or file-id:ID.

Example:
  grok-video extend --video source.mp4 --prompt "The camera pulls back" --duration 3 --output extended.mp4`,
	"status": `GET /videos/REQUEST_ID exactly once. Writes upstream JSON plus request_id.
Exit 0 means the query succeeded, even if status is pending/failed/expired.
No polling or downloading. Missing/invalid JSON status is a protocol error.

Example:
  grok-video status REQUEST_ID --base-url https://video-api.example.com/v1`,
	"wait": `Poll GET /videos/REQUEST_ID while status=pending, until done/failed/expired.
Writes final upstream JSON plus request_id; remote failure also exits 4.
Unknown statuses fail explicitly. Progress is shown on stderr when changed.
Optional --output downloads once after completion; no local job database.
--wait-timeout starts now; download has its separate per-request timeout.

Example:
  grok-video wait REQUEST_ID --wait-timeout 15m --output result.mp4`,
	"download": `Fetch the result and download video.url to required --output FILE.
Without --wait, a pending/failed/expired/filtered job exits 4.
With --wait, polls until completion first. Requires an MP4 ftyp header.
Parent directory must exist. Existing files are preserved unless --overwrite.
Publishes a completed temporary file atomically; no partial final outputs.
stdout: upstream JSON plus request_id and download:{path,bytes}.
Download and redirects carry NO API key; HTTPS downgrade redirects fail.

Example:
  grok-video download REQUEST_ID --output clip.mp4
  grok-video download REQUEST_ID --wait --output clip.mp4 --overwrite`,
	"models": `GET /models and filter data[].id by --prefix (default grok-imagine-video).
Listing a model does not prove every parameter/capability works.
Outputs the catalog JSON with the filtered data array. No generation occurs.

Examples:
  grok-video models
  grok-video models --prefix= --base-url https://video-api.example.com/v1`,
}
