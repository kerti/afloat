package dev.kerti.afloat.testsupport

import org.springframework.jdbc.core.simple.JdbcClient
import java.math.BigDecimal
import java.security.SecureRandom
import java.time.Instant
import java.time.LocalTime
import java.util.UUID
import javax.sql.DataSource

// Rows are seeded through JdbcClient rather than the repositories: a fixture
// that goes through JPA would assert the mapping it is meant to be independent
// of, and Session.isNew() makes save() a subject under test, not a tool.
object AuthFixtures {

    // The plaintext every fixture credential hashes, and a PHC string for it
    // written by the GO backend (backend/internal/auth.HashPassword). Reusing a
    // fixed hash keeps seeding free - hashing per test would cost 19 MiB and
    // ~50ms each - and makes every login test cross-backend evidence as well.
    const val PASSWORD = "correct horse battery staple"
    const val PASSWORD_PHC =
        "\$argon2id\$v=19\$m=19456,t=2,p=1\$cAz2vjLpthahaEGhmk5fQQ\$xSslbUYj+Sx4LouRFXtaZmG8Wwy1Y5Iz6b3q2S76Da0"

    private val random = SecureRandom()

    // UUIDv7, client-generated, per BOOTSTRAP.md §4 non-negotiable 2. The JDK
    // has no generator for it, and a v4 here would let a fixture pass that a
    // real client-supplied id would not.
    fun uuidV7(at: Instant = Instant.now()): UUID {
        val bytes = ByteArray(16)
        random.nextBytes(bytes)
        val millis = at.toEpochMilli()
        for (i in 0..5) bytes[i] = (millis shr (40 - 8 * i)).toByte()
        bytes[6] = ((bytes[6].toInt() and 0x0F) or 0x70).toByte()  // version 7
        bytes[8] = ((bytes[8].toInt() and 0x3F) or 0x80).toByte()  // variant
        var msb = 0L
        var lsb = 0L
        for (i in 0..7) msb = (msb shl 8) or (bytes[i].toLong() and 0xFF)
        for (i in 8..15) lsb = (lsb shl 8) or (bytes[i].toLong() and 0xFF)
        return UUID(msb, lsb)
    }

    fun household(
        dataSource: DataSource,
        displayName: String = "Rumah Tangga",
        reportingCurrency: String = "IDR",
        periodStartDay: Int = 1,
        dayStartsAt: LocalTime = LocalTime.of(4, 0),
        expectedMonthlyIncome: BigDecimal? = null,
        allowanceMode: String = "adaptive",
        deletedAt: Instant? = null,
    ): UUID {
        val id = uuidV7()
        JdbcClient.create(dataSource).sql(
            """
            INSERT INTO households (id, display_name, reporting_currency, period_start_day,
                                    day_starts_at, expected_monthly_income, allowance_mode, deleted_at)
            VALUES (:id, :displayName, :reportingCurrency, :periodStartDay,
                    :dayStartsAt, :expectedMonthlyIncome, :allowanceMode, :deletedAt)
            """
        ).param("id", id)
            .param("displayName", displayName)
            .param("reportingCurrency", reportingCurrency)
            .param("periodStartDay", periodStartDay)
            .param("dayStartsAt", dayStartsAt)
            .param("expectedMonthlyIncome", expectedMonthlyIncome)
            .param("allowanceMode", allowanceMode)
            .param("deletedAt", deletedAt?.let { java.sql.Timestamp.from(it) })
            .update()
        return id
    }

    fun user(
        dataSource: DataSource,
        householdId: UUID,
        email: String = "user@example.com",
        displayName: String = "Rad",
        locale: String = "en-GB",
        timeZone: String = "Asia/Jakarta",
        deletedAt: Instant? = null,
    ): UUID {
        val id = uuidV7()
        JdbcClient.create(dataSource).sql(
            """
            INSERT INTO users (id, household_id, email, display_name, locale, time_zone, deleted_at)
            VALUES (:id, :householdId, :email, :displayName, :locale, :timeZone, :deletedAt)
            """
        ).param("id", id)
            .param("householdId", householdId)
            .param("email", email)
            .param("displayName", displayName)
            .param("locale", locale)
            .param("timeZone", timeZone)
            .param("deletedAt", deletedAt?.let { java.sql.Timestamp.from(it) })
            .update()
        return id
    }

    fun credential(dataSource: DataSource, userId: UUID, passwordHash: String = PASSWORD_PHC) {
        JdbcClient.create(dataSource).sql(
            "INSERT INTO credentials (user_id, password_hash) VALUES (:userId, :hash)"
        ).param("userId", userId).param("hash", passwordHash).update()
    }

    // A Household, a User and a credential in one call: what almost every spec
    // here needs before it can log in.
    data class Account(val householdId: UUID, val userId: UUID, val email: String)

    fun account(
        dataSource: DataSource,
        email: String = "user@example.com",
        passwordHash: String = PASSWORD_PHC,
        withCredential: Boolean = true,
        userDeletedAt: Instant? = null,
        householdDeletedAt: Instant? = null,
        expectedMonthlyIncome: BigDecimal? = null,
        locale: String = "en-GB",
        periodStartDay: Int = 1,
        dayStartsAt: LocalTime = LocalTime.of(4, 0),
        allowanceMode: String = "adaptive",
        householdDisplayName: String = "Rumah Tangga",
        reportingCurrency: String = "IDR",
    ): Account {
        val householdId = household(
            dataSource,
            displayName = householdDisplayName,
            reportingCurrency = reportingCurrency,
            periodStartDay = periodStartDay,
            dayStartsAt = dayStartsAt,
            expectedMonthlyIncome = expectedMonthlyIncome,
            allowanceMode = allowanceMode,
            deletedAt = householdDeletedAt,
        )
        val userId = user(dataSource, householdId, email = email, locale = locale, deletedAt = userDeletedAt)
        if (withCredential) credential(dataSource, userId, passwordHash)
        return Account(householdId, userId, email)
    }

    // Seeds a session row directly, so a spec can place created_at and
    // expires_at wherever it needs them without waiting or moving a Clock.
    fun session(
        dataSource: DataSource,
        userId: UUID,
        tokenHash: String,
        createdAt: Instant = Instant.now(),
        expiresAt: Instant = Instant.now().plusSeconds(3600),
        lastSeenAt: Instant = Instant.now(),
        userAgent: String? = null,
    ) {
        JdbcClient.create(dataSource).sql(
            """
            INSERT INTO sessions (id, user_id, created_at, expires_at, last_seen_at, user_agent)
            VALUES (:id, :userId, :createdAt, :expiresAt, :lastSeenAt, :userAgent)
            """
        ).param("id", tokenHash)
            .param("userId", userId)
            .param("createdAt", java.sql.Timestamp.from(createdAt))
            .param("expiresAt", java.sql.Timestamp.from(expiresAt))
            .param("lastSeenAt", java.sql.Timestamp.from(lastSeenAt))
            .param("userAgent", userAgent)
            .update()
        return
    }

    fun loginAttempt(
        dataSource: DataSource,
        key: String,
        failureCount: Int = 1,
        backoffUntil: Instant? = null,
    ) {
        JdbcClient.create(dataSource).sql(
            """
            INSERT INTO login_attempts (key, failure_count, backoff_until)
            VALUES (:key, :failureCount, :backoffUntil)
            """
        ).param("key", key)
            .param("failureCount", failureCount)
            .param("backoffUntil", backoffUntil?.let { java.sql.Timestamp.from(it) })
            .update()
    }
}
