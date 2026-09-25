package dev.kerti.afloat.config

import io.kotest.assertions.throwables.shouldThrow
import io.kotest.core.spec.style.StringSpec
import io.kotest.datatest.withData
import io.kotest.matchers.shouldBe

data class PostgresUrlTranslatorCase(
    val name: String,
    val input: String,
    val expectedJdbc: String,
    val expectedUser: String?,
    val expectedPass: String?,
)

// databaseUrlIsTranslatedToJdbc (#13 test 5)
//
// DATABASE_URL is the one §12 name that cannot be shared verbatim — libpq for
// pgx, JDBC for Spring — so it is translated at boot rather than spelled twice.
open class PostgresUrlTranslatorSpec : StringSpec({

    withData(
        nameFn = { "Accepts ${it.name}" },
        listOf(
            PostgresUrlTranslatorCase(
                "jdbc connection string (as is)",
                "jdbc:postgresql://localhost:5184/afloat_kotlin",
                "jdbc:postgresql://localhost:5184/afloat_kotlin",
                null,
                null
            ),
            PostgresUrlTranslatorCase(
                "standard postgres connection string",
                "postgres://host:5184/afloat_kotlin",
                "jdbc:postgresql://host:5184/afloat_kotlin",
                null,
                null
            ),
            PostgresUrlTranslatorCase(
                "without port number",
                "postgres://host/afloat_kotlin",
                "jdbc:postgresql://host/afloat_kotlin",
                null,
                null
            ),
            PostgresUrlTranslatorCase(
                "standard postgresql connection string",
                "postgresql://host:5184/afloat_kotlin",
                "jdbc:postgresql://host:5184/afloat_kotlin",
                null,
                null
            ),
            PostgresUrlTranslatorCase(
                "with percent-formatted '@' and '+' in username and password",
                "postgresql://us%40%2Ber:pa%40%2Bss@host:5184/afloat_kotlin",
                "jdbc:postgresql://host:5184/afloat_kotlin",
                "us@+er",
                "pa@+ss"
            ),
            PostgresUrlTranslatorCase(
                "with bare '@' and '+' in username and password",
                "postgresql://us%40+er:pa%40%2Bss@host:5184/afloat_kotlin",
                "jdbc:postgresql://host:5184/afloat_kotlin",
                "us@+er",
                "pa@+ss"
            ),
            PostgresUrlTranslatorCase(
                "full connection string with parameter",
                "postgres://user:pass@host:5184/afloat_kotlin?sslmode=require",
                "jdbc:postgresql://host:5184/afloat_kotlin?sslmode=require",
                "user",
                "pass"
            ),
            PostgresUrlTranslatorCase(
                "username with empty password",
                "postgres://user:@host:5184/afloat_kotlin?sslmode=require",
                "jdbc:postgresql://host:5184/afloat_kotlin?sslmode=require",
                "user",
                ""
            ),
            PostgresUrlTranslatorCase(
                "username without password",
                "postgres://user@host:5184/afloat_kotlin?sslmode=require",
                "jdbc:postgresql://host:5184/afloat_kotlin?sslmode=require",
                "user",
                null
            ),
            PostgresUrlTranslatorCase(
                "IPv6 hostname",
                "postgres://[2001:db8::1234]:5184/afloat_kotlin",
                "jdbc:postgresql://[2001:db8::1234]:5184/afloat_kotlin",
                null,
                null
            ),
        ),
    ) { (_, input, expectedJdbc, expectedUser, expectedPass) ->
        val (parsed, user, pass) = PostgresUrlTranslator.translate(input)
        parsed shouldBe expectedJdbc
        user shouldBe expectedUser
        pass shouldBe expectedPass
    }

    withData(
        nameFn = { "Rejects '$it'" },
        listOf(
            "",
            "garbage",
            "postgresql://:5184/afloat_kotlin",
            "postgresql://:5184/",
            "mysql://host:5184/afloat_kotlin",
            "postgres://host:99999/afloat_kotlin",
            // libpq's Unix-socket spelling, which pgx takes. Refused by
            // decision, not by accident (#32 item 7, BOOTSTRAP.md §12).
            "postgres:///afloat_kotlin?host=/var/run/postgresql",
        )
    ) { input ->
        shouldThrow<IllegalArgumentException> { PostgresUrlTranslator.translate(input) }
    }
})
