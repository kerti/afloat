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

    private fun escape(s: String): String = s
        .replace("\\", "\\\\")
        .replace("\"", "\\\"")
}
