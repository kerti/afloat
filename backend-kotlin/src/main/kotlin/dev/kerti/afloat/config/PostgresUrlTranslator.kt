package dev.kerti.afloat.config

import java.net.URLDecoder

data class JdbcSource(val jdbcUrl: String, val username: String?, val password: String?)

object PostgresUrlTranslator {

    private val urlPattern = """^postgres(?:ql)?://(?:(?<user>[^:@]+)(?::(?<pass>[^@]*))?@)?(?<host>[^/:]+|\[[a-fA-F0-9:.]+])(?::(?<port>\d+))?/(?<db>[^\s?]+)(?:\?(?<params>\S*))?$""".toRegex()

    /*
    Pure function to translate PostgreSQL connection strings into a JDBC-compatible form.
    1. JDBC connection strings are untouched.
    2. Parse using libpq standard.
    3. Strip the credentials and convert to fields.
    4. Pass any URL queries as is.
     */
    fun translate(input: String): JdbcSource {
        if (input.isEmpty()) {
            throw IllegalArgumentException("Database connection string must not be empty")
        }

        if (input.startsWith("jdbc:")) {
            return JdbcSource(input, null, null)
        }

        val match = urlPattern.find(input) ?: throw IllegalArgumentException("Invalid database connection string")

        val user = match.groups["user"]?.value
        val pass = match.groups["pass"]?.value
        val host = match.groups["host"]?.value ?: throw IllegalArgumentException("Host must be provided")
        val port = match.groups["port"]?.value
        val db = match.groups["db"]?.value ?: throw IllegalArgumentException("Database name must be provided")
        val params = match.groups["params"]?.value

        if (port != null && port.toIntOrNull()?.let { it in 1..65535 } != true) {
            throw IllegalArgumentException("Port '$port' is not a valid port")
        }

        val decodedUser: String? = user?.let { URLDecoder.decode(user.replace("+", "%2B"), Charsets.UTF_8) }
        val decodedPass: String? = pass?.let { URLDecoder.decode(pass.replace("+", "%2B"), Charsets.UTF_8) }

        val jdbcUrl = "jdbc:postgresql://$host${port?.let { ":$it" } ?: ""}/${db}${params?.let { "?$it" } ?: ""}"
        return JdbcSource(jdbcUrl = jdbcUrl, username = decodedUser, password = decodedPass)
    }
}