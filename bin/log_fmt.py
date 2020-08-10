#!/usr/bin/env python3

__doc__ = """\
formats json log lines with colors and stuff.

special formatting for these three fields, which must exist:
level, message, time.

uses color according to the --color argument and whether stdout is a tty.
"""

import sys
import os
import re
import json
import datetime

try:
    from json.decoder import JSONDecodeError
except ImportError:
    # compatibility for pre Python 3.5
    JSONDecodeError = ValueError

def main():
    import argparse

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--color",
        choices=["always", "never", "auto"],
        default="auto",
        help="auto, the default, tests if stdout is a tty",
    )
    parser.add_argument(
        "--test",
        action="store_true",
        help="instead of reading from stdin, use a preset list of lines "
        "to test this programs formatting functionality, then exit.",
    )
    parser.add_argument(
        "--quiet",
        "-q",
        action="store_true",
        help="omit process, file, line, func"
    )
    args = parser.parse_args()

    if args.test:
        line_producer = [
            json.dumps(dict({
                # these are the same on every line
                "file": "main.go",
                "func": "configure()",
                "line": "53",
                "process": "example-service",
                "msgTemplate": j["msg"],
            }, **j)) for j in [
                # these are different on every line
                {"level":"trace","time":"2019-03-27T15:36:49.481765984-04:00","msg":"starting test output"},
                {"level":"debug","time":"2019-03-27T15:36:50.000000000-04:00","msg":"is this thing on?","arg_name":"value"},
                {"level": "info","time":"2019-03-27T23:36:51+04:00"          ,"msg":"test is running","arg_a":"a","arg_b":"b","arg_c":"hello","arg_d":"1,2"},
                {"level": "warn","time":"2019-03-27T19:36:52.48Z"            ,"msg":"error with no args","error":"Error message"},
                {"level":"error","time":"2019-03-27T15:36:53.481765984-04:00","msg":"weird characters in message \n \'\" \\ {}","arg_name":"arg value","error":"Another error message"},
                {"level":"fatal","time":"2019-03-27T15:36:54.481765984-04:00","msg":"test is over!"},
            ]
        ]
    else:
        line_producer = sys.stdin

    fmt_to_stdout(line_producer, args.color, args.quiet)

def fmt_to_stdout(line_producer, color_setting="auto", quiet=False):
    """
    This function can be called from other scripts.
    line_producer will be iterated to produce lines of json.
    color_setting should be one of ("auto", "always", or "never").
    """

    if color_setting == "always":
        use_color = True
    elif color_setting == "never":
        use_color = False
    elif color_setting == "auto":
        use_color = os.isatty(sys.stdout.fileno())
    else:
        assert False

    for line in line_producer:
        try:
            fields = json.loads(line)
        except JSONDecodeError:
            fields = None
        if type(fields) != dict or "msgTemplate" not in fields:
            # this line is not our json log. just print it as-is
            sys.stdout.write(line)
            continue

        # extract the required fields
        level = fields["level"]
        level_formatted = "{:>5}".format(level)
        if use_color:
            color_styles = level_styles[level]
            level_formatted = color(level_formatted, *color_styles)

        file_name = color(fields["file"], FG_PURPLE)
        func_name = color(fields["func"], FG_PURPLE)
        line_num = color(fields["line"], FG_PURPLE)
        process_name = color(fields["process"], FG_PURPLE)

        message = fields["msg"]

        if "debug" != level and "trace" != level:
            message = sanitize_message(message)

        timestamp = fields["time"]
        timestamp = format_time(timestamp)
        if use_color:
            timestamp = color(timestamp, FADED)

        extra_args = {}
        for k, v in fields.items():
            if k.startswith("arg_"):
                extra_args[k[len("arg_"):]] = v

        try:
            error = fields["error"]
        except KeyError:
            error = None
        else:
            error = "error: " + error
            if use_color:
                error = color(error, *color_styles)

        if quiet:
            fmt = "{}: {}: {}"
            fmt_args = [timestamp, level_formatted, message]
        else:
            fmt = "{}: {}: {}: {}:{} {}: {}"
            fmt_args = [timestamp, level_formatted, process_name, file_name, line_num,
                        func_name, message]

        if len(extra_args) > 0 or error != None:
            fmt += " {}"
            fmt_args.append(json.dumps(extra_args))
        if error != None:
            fmt += " {}"
            fmt_args.append(error)

        print(fmt.format(*fmt_args))
        if use_color:
            sys.stdout.flush()


# escape all ascii control codes, including newline, hard tab, escape (which does terminal colors), etc.
# escape the weird Unicode ways to do newlines, since that would disrupt the line-oriented output.
# escape "double quotes", so these messages will end up looking like string literal contents (copy-paste friendly).
# don't escape 'single quotes', because --> that's <-- common in messages and would be distracting.
# escape '{', because that character starts the args part of the line.
_bad_chars = re.compile(r"[\x00-\x1f\x7f\u0085\u2028\u2029\x22\\{]")
special_escapes = {
    # these characters are not escaped by repr(str), so escape them manually.
    '"': '\\"',
    "{": "\\x7b",
}


def _replace_function(match):
    # this function will only run when a bad character is found, which is infrequent.
    c = match.group()  # m <3
    try:
        return special_escapes[c]
    except KeyError:
        # use backslash notation, and strip the quotes
        return repr(c)[1:-1]


def sanitize_message(message):
    """
    make sure there are no newlines or other terminal characters
    that could cause problems when printed in a line of terminal output
    """
    # i tried to keep this function as lean as possible since it's on a hot path.
    return _bad_chars.sub(_replace_function, message)


def format_time(s):
    timestamp = parse_rfc3339_datetime(s)
    return timestamp.strftime("%H:%M:%S.%f")


BOLD = 1
FADED = 2
FG_RED = 31
FG_AMBER = 33
FG_PURPLE = 35
FG_CYAN = 36
BG_RED = 41
reset_sequence = "\x1b[m"


def color(text, *styles):
    return "{}{}{}".format(
        "\x1b[{}m".format(";".join(str(c)
                                   for c in styles)), text, reset_sequence
    )

level_styles = {
    "trace": (FADED,),
    "debug": (FG_RED,),
    "info": (FG_CYAN,),
    "warn": (FG_AMBER,),
    "error": (BOLD, FG_RED),
    "fatal": (BOLD, BG_RED),
}


# This is my life now. I'm writing a standards-compliant parser
# for RFC3339, because the one in the Python standard library only
# accepts 3 or 6 decimal digits in the fractional part of the seconds
# ( https://github.com/python/cpython/blob/336b3064d8981bc7f76c5cc6f6a0527df69771d6/Lib/datetime.py#L306-L307 )
# instead of any number of digits as required by the spec
# (the spec even gives an example with 2 digits.).
# And our Go logging library uses 9 digits.
time_pattern = re.compile(
    r"^"
    r"(\d{4})-(\d{2})-(\d{2})"  # year, month, day
    r"T"
    r"(\d{2}):(\d{2}):(\d{2})"  # hour, minute, second
    r"(\.\d+)?"  # fractions of a second
    r"(?:Z|([+-]\d{2}):(\d{2}))"  # timezone hour, timezone minute
    r"$"
)


def parse_rfc3339_datetime(s):
    """
    See https://tools.ietf.org/html/rfc3339#section-5.6

    returns a datetime, or None if there is a parsing error.
    fractional seconds are truncated to the microsecond.
    """
    match = time_pattern.match(s)
    if match == None:
        return None

    (
        year,
        month,
        day,
        hour,
        minute,
        second,
        second_fraction,
        tzhour,
        tzminute,
    ) = match.groups()  # m <3

    if second_fraction:
        microseconds = int(float("0" + second_fraction) * 1000 * 1000)
    else:
        microseconds = 0

    if tzhour:
        timezone = datetime.timezone(
            datetime.timedelta(hours=int(tzhour), minutes=int(tzminute))
        )
    else:
        # "Z" instead of a timezone means +00:00
        timezone = datetime.timezone.utc

    return datetime.datetime(
        int(year),
        int(month),
        int(day),
        int(hour),
        int(minute),
        int(second),
        microseconds,
        timezone,
    ).astimezone()


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        pass
