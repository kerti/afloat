package dev.kerti.afloat.auth

import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Base64

object TokenService {
    private val secureRandom = SecureRandom()

    // A fresh 256-bit URL-safe opaque token. Only its SHA-256 is stored.
    fun issue(): Pair<String, String> {
        val bytes = ByteArray(32)
        secureRandom.nextBytes(bytes)
        val token = Base64.getUrlEncoder().withoutPadding().encodeToString(bytes)
        return token to hash(token)
    }

    fun hash(token: String): String {
        val digest = MessageDigest.getInstance("SHA-256").digest(token.toByteArray(Charsets.UTF_8))
        return digest.joinToString("") { "%02x".format(it.toInt() and 0xFF) }
    }
}
