package dev.kerti.afloat.testsupport

import dev.kerti.afloat.auth.SessionCookieFactory
import org.springframework.http.HttpHeaders
import org.springframework.test.web.servlet.MvcResult
import java.security.MessageDigest

// SHA-256 spelled out here rather than called through TokenService: a test that
// hashes with the code under test proves only that it is self-consistent, and
// test 14 is about what reaches the sessions table.
fun sha256Hex(value: String): String =
    MessageDigest.getInstance("SHA-256")
        .digest(value.toByteArray(Charsets.UTF_8))
        .joinToString("") { String.format("%02x", it) }

fun MvcResult.setCookieHeaders(): List<String> = response.getHeaders(HttpHeaders.SET_COOKIE)

// The Set-Cookie header for afloat_session, or null when the response set none.
fun MvcResult.sessionCookieHeader(): String? =
    setCookieHeaders().firstOrNull { it.startsWith("${SessionCookieFactory.COOKIE_NAME}=") }

// The token the browser would start presenting: the part before the first ';'.
fun String.cookieValue(): String = substringBefore(';').substringAfter('=')

// Attribute name (lowercased) -> value, with flag attributes mapped to "".
// Path=/ and HttpOnly both land here, so a spec can assert on either shape.
fun String.cookieAttributes(): Map<String, String> =
    split(';').drop(1).associate { segment ->
        val trimmed = segment.trim()
        val name = trimmed.substringBefore('=').lowercase()
        name to trimmed.substringAfter('=', "")
    }

fun loginBody(email: String, password: String): String =
    """{"email":${email.jsonString()},"password":${password.jsonString()}}"""

private fun String.jsonString(): String =
    "\"" + replace("\\", "\\\\").replace("\"", "\\\"") + "\""

// A flat JSON object read WITHOUT a mapper, as name -> raw value text: a string
// keeps its quotes, a number and a boolean do not. Deliberately not
// ObjectMapper.readValue, because these assertions are about the bytes on the
// wire — `additionalProperties: false`, money as a quoted string, a null field
// omitted rather than emitted — and a mapper answers about how IT chose to read
// them back. Extracted from MeSpec so the system routes assert the same way.
fun String.jsonFields(): Map<String, String> {
    val raw = trim().removePrefix("{").removeSuffix("}")
    if (raw.isBlank()) return emptyMap()
    return raw.splitTopLevel().associate { pair ->
        pair.substringBefore(':').trim().trim('"') to pair.substringAfter(':').trim()
    }
}

// Splits on commas that are not inside a quoted string.
private fun String.splitTopLevel(): List<String> {
    val parts = mutableListOf<String>()
    val current = StringBuilder()
    var inQuotes = false
    forEach { c ->
        when {
            c == '"' -> { inQuotes = !inQuotes; current.append(c) }
            c == ',' && !inQuotes -> { parts += current.toString(); current.clear() }
            else -> current.append(c)
        }
    }
    if (current.isNotBlank()) parts += current.toString()
    return parts
}
