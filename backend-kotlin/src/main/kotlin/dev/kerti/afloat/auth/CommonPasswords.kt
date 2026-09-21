package dev.kerti.afloat.auth

// The denylist is the single most effective password rule available: it rejects
// what attackers actually try first, without asking anyone to remember a symbol.
//
// The file is a COPY. shared/common_passwords.txt is canonical and is copied in
// by scripts/sync-denylist.sh, gated by `make check` — both backends reject the
// same passwords or they are not the same app, and a denylist that drifts is a
// divergence no contract test can see. Never hand-edit the copy.
object CommonPasswords {

    private const val RESOURCE = "/common_passwords.txt"

    private val entries: Set<String> by lazy {
        val stream = checkNotNull(CommonPasswords::class.java.getResourceAsStream(RESOURCE)) {
            // Failing at first use rather than degrading to an empty set: a
            // silently absent denylist accepts every breached password and
            // nothing in the response says so.
            "$RESOURCE is missing from the classpath - run make sync-denylist"
        }
        stream.bufferedReader().useLines { lines ->
            lines.map { it.trim().lowercase() }
                .filter { it.isNotEmpty() && !it.startsWith("#") }
                .toSet()
        }
    }

    // Compared case-insensitively: Password1234 and password1234 are the same
    // guess, and an attacker tries both.
    fun contains(password: String): Boolean = password.lowercase() in entries
}
