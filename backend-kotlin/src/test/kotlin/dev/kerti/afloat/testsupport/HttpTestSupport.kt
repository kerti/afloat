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
