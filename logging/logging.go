// Output structure:
// Each line of output is a json object starting with '{' and ending with '}'
// Each line object has string values with the following structure:
// {
//   // The message
//   "msg": "text that contains a value", // the processed result of the template with the args.
//   "msgTemplate": "text that contains a {{.variable}}", // contains the unprocessed template
//   "arg_variable": "value", // example of an arg named "variable" with value "value"
//   ["arg_<name>": "<value>", ...], // 0 or more arg properties with names starting with "arg_".
//
//   // Caller-supplied metadata
//   "level": "info", // one of: ["trace", "debug", "info", "warn", "error", "fatal"]
//   ["error": "error message"], // optional "error" field if present is a string error message.
//
//   // Context fields that get filled in automatically
//   "time": "2006-01-02T15:04:05.123456789-07:00", // RFC3339Nano
//   "file": "main.go",
//   "func": "ServeGrpc()",
//   "line": "59", // note that this is a string.
//   "process": "sms-auth-service", // executable name, no slash.
// }
package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/template"
	"time"
)

type Args map[string]string

type Logger struct {
	Level   string
	IsFatal bool
}

func (self *Logger) Log(msg string) {
	self.logGenericArgs(msg, nil, nil, 1)
}

func (self *Logger) LogArgs(msgTemplate string, args Args) {
	self.logGenericArgs(msgTemplate, nil, args, 1)
}

func (self *Logger) LogErr(msg string, err error) {
	self.logGenericArgs(msg, err, nil, 1)
}

func (self *Logger) LogErrArgs(msgTemplate string, err error, args Args) {
	self.logGenericArgs(msgTemplate, err, args, 1)
}

// If args is nil, then msgTemplate is not really a template; it's just the msg.
// stackDepth is the distance from the callee's stack frame to the stack frame
// of the user code that called into our humble logger
func (self *Logger) logGenericArgs(msgTemplate string, err error, args Args, stackDepth int) {
	file, func_, line := GetStackInfo(stackDepth + 1)
	msg := msgTemplate
	if args != nil {
		t, templateErr := template.New("").Parse(msgTemplate)
		if templateErr != nil {
			// While we're sure this is the developer's fault,
			// and this is typically the kind of scenario where we'd panic at yell at them,
			// let's not panic here, because it's especially easy to have logging code
			// that is hard to test (certain kinds of error reporting, for example).
			// Instead let's make the best of the situation.
			args["_templateErr"] = templateErr.Error()
		} else {
			var buf bytes.Buffer
			templateErr := t.Execute(&buf, args)
			if templateErr != nil {
				// see above comment about panicking.
				args["_templateErr"] = templateErr.Error()
			} else {
				msg = buf.String()
			}
		}
	}

	fullArgs := Args{
		"msgTemplate": msgTemplate,
		"msg":         msg,
		"time":        time.Now().Format(time.RFC3339Nano),
		"level":       self.Level,
		"file":        file,
		"func":        func_,
		"line":        line,
		"process":     selfExeName,
	}

	for k, v := range args {
		fullArgs["arg_"+k] = v
	}

	if err != nil {
		fullArgs["error"] = err.Error()
	}

	jsonWriter.Encode(fullArgs)

	if self.IsFatal {
		panic(msg)
	}
}

func GetStackInfo(stackDepth int) (string, string, string) {
	result_file := "?"
	result_func := "?()"
	result_line := "0"

	if pc, file, line, ok := runtime.Caller(stackDepth + 1); ok {
		result_file = filepath.Base(file)
		result_line = Int(line)
		if fn := runtime.FuncForPC(pc); fn != nil {
			dotName := filepath.Ext(fn.Name())
			result_func = strings.TrimLeft(dotName, ".") + "()"
		}
	}
	return result_file, result_func, result_line
}

var (
	jsonWriter *json.Encoder

	traceLogger *Logger
	debugLogger *Logger
	infoLogger  *Logger
	warnLogger  *Logger
	errorLogger *Logger
	fatalLogger *Logger

	selfExeName string
)

func init() {
	jsonWriter = json.NewEncoder(os.Stdout)
	jsonWriter.SetEscapeHTML(false)
	jsonWriter.SetIndent("", "")

	// These string representations match the ones for fluentd:
	// https://docs.fluentd.org/v1.0/articles/logging#log-level
	traceLogger = &Logger{Level: "trace", IsFatal: false}
	debugLogger = &Logger{Level: "debug", IsFatal: false}
	infoLogger = &Logger{Level: "info", IsFatal: false}
	warnLogger = &Logger{Level: "warn", IsFatal: false}
	errorLogger = &Logger{Level: "error", IsFatal: false}
	fatalLogger = &Logger{Level: "fatal", IsFatal: true}

	selfExeName = filepath.Base(os.Args[0])
}

func Trace() *Logger {
	return traceLogger
}

func Debug() *Logger {
	return debugLogger
}

func Info() *Logger {
	return infoLogger
}

func Warn() *Logger {
	return warnLogger
}

func Error() *Logger {
	return errorLogger
}

func Fatal() *Logger {
	return fatalLogger
}

// convenience functions for converting things to string

// Converts a valid value to a JSON string. Channels, complex numbers, and
// functions are not supported types. Floats are fine except for +Inf, -Inf,
// and NaN. Maps work if the key is a string or integer. Cyclic structures are
// right out. Pretty much everything else is fair game.
// Read more: https://golang.org/pkg/encoding/json/#Marshal
func Json(j interface{}) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "")
	err := encoder.Encode(j)

	if err == nil {
		return strings.TrimSuffix(buf.String(), "\n")
	}

	// j could not be serialized to json, so let's log the error and return a
	// helpful-ish value
	Error().logGenericArgs("error serializing value to json", err, nil, 1)
	return fmt.Sprintf("<error: %v>", err)
}
func Int(i int) string {
	// The "a" is short for "string", obviously.
	return strconv.Itoa(i)
}
func Int64(i int64) string {
	// Base 10
	return strconv.FormatInt(i, 10)
}
func Int32(i int32) string {
	// Base 10
	return strconv.FormatInt(int64(i), 10)
}
func Uint32(i uint32) string {
	// Base 10
	return strconv.FormatUint(uint64(i), 10)
}
func Uint64(i uint64) string {
	// Base 10
	return strconv.FormatUint(i, 10)
}
func Float64(i float64) string {
	return strconv.FormatFloat(i, 'f', -1, 64)
}
func Bool(b bool) string {
	return strconv.FormatBool(b)
}
func Duration(d time.Duration) string {
	return d.String()
}
func Time(t time.Time) string {
	return t.Local().Format(time.RFC3339Nano)
}
