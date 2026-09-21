package dev.kerti.afloat.httperr

import dev.kerti.afloat.api.model.ErrorCode
import jakarta.servlet.http.HttpServletResponse

// Writes the one error envelope both backends emit: {"code": "X", "args":{...}},
// no "message" field. Used by filters and the entry point, which hold a raw
// HttpServletResponse instead of a controller to serialize through Boot's
// ObjectMapper. Args are flat JSON primitives only (the contract says so).
object ApiErrorWriter {
    fun write(response: HttpServletResponse, status: Int, code: ErrorCode, args: Map<String, Any>? = null) {
        if (response.isCommitted) return
        response.status = status
        // Charset set explicitly. Without it the servlet default is ISO-8859-1,
        // so the first arg carrying a non-ASCII character — an Indonesian
        // display name, say — reaches the client mangled, and nothing here or
        // in the frontend would say why.
        response.characterEncoding = Charsets.UTF_8.name()
        response.contentType = "application/json"
        val body = StringBuilder("{\"code\":\"${code.value}\"")
        if (!args.isNullOrEmpty()) {
            body.append(",\"args\":").append(valueToJson(args))
        }
        body.append('}')
        response.writer.write(body.toString())
    }

    private fun valueToJson(value: Any?): String = when (value) {
        null -> "null"
        is String -> "\"${escape(value)}\""
        is Boolean -> value.toString()
        is Number -> value.toString()
        else -> "\"" + escape(value.toString()) + "\""
    }

    private fun valueToJson(map: Map<String, Any>): String =
        map.entries.joinToString(",", "{", "}") { (k, v) ->
            "\"${escape(k)}\":" + valueToJson(v)
        }

    // RFC 8259 §7. The two-replace version this had covered `\` and `"` and
    // emitted a raw newline or tab straight into the body, which is invalid
    // JSON: the client's parse fails and the error it was carrying is lost
    // along with it. Only reachable through args today, and args are only ever
    // written by the filters — which is exactly the sort of "unreachable" that
    // stops being true quietly.
    private fun escape(s: String): String = buildString(s.length) {
        s.forEach { c ->
            when {
                c == '\\' -> append("\\\\")
                c == '"' -> append("\\\"")
                c == '\n' -> append("\\n")
                c == '\r' -> append("\\r")
                c == '\t' -> append("\\t")
                c == '\b' -> append("\\b")
                c == '\u000C' -> append("\\f")
                c < ' ' -> append("\\u%04x".format(c.code))
                else -> append(c)
            }
        }
    }
}
