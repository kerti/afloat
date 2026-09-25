package dev.kerti.afloat.auth

import dev.kerti.afloat.api.model.ErrorCode
import dev.kerti.afloat.api.model.Locale
import dev.kerti.afloat.api.model.Me
import dev.kerti.afloat.auth.data.CredentialRepository
import dev.kerti.afloat.auth.data.HouseholdRepository
import dev.kerti.afloat.auth.data.LoginAttemptRepository
import dev.kerti.afloat.auth.data.Session
import dev.kerti.afloat.auth.data.SessionRepository
import dev.kerti.afloat.auth.data.User
import dev.kerti.afloat.auth.data.UserRepository
import dev.kerti.afloat.config.AppConfig
import dev.kerti.afloat.httperr.ApiException
import dev.kerti.afloat.httperr.trimSpace
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Service
import org.springframework.transaction.annotation.Transactional
import java.time.Clock
import java.time.LocalTime
import java.time.format.DateTimeFormatter
import java.util.UUID

@Service
class AuthService(
    private val userRepository: UserRepository,
    private val credentialRepository: CredentialRepository,
    private val sessionRepository: SessionRepository,
    private val loginAttemptRepository: LoginAttemptRepository,
    private val householdRepository: HouseholdRepository,
    private val passwordService: PasswordService,
    private val sessionCookieFactory: SessionCookieFactory,
    private val clock: Clock,
    private val appConfig: AppConfig,
) {
    data class Issue(val cookie: String)

    // Every failure mode: unknown email (no credential, wrong password) is one
    // INVALID_CREDENTIALS, the compare is constant work, and an unknown address
    // still pays the hash.
    //
    // Deliberately NOT @Transactional. A transaction spanning the verify pins a
    // pooled connection for all of it, and the verify includes the wait for an
    // Argon2 permit (#33), up to HTTP_WRITE_TIMEOUT: a login flood past the cap
    // would park a connection per queued login and drain the pool for every
    // endpoint. Each repository call is its own short transaction instead,
    // which is also what makes the "logged, never raised" writes below
    // survivable: one failed statement cannot abort the ones after it.
    fun login(email: String, password: String): Issue {
        val normalized = normalizeEmail(email)
        val keys = backoffKeys(normalized)

        // Read once before queueing, so a throttled caller is answered without
        // waiting for a permit. checkPassword reads it again under the permit.
        refuseWhileThrottled(keys)

        val (user, phc) = resolve(normalized)
        if (!checkPassword(keys, password, phc) || user == null) {
            throw ApiException(401, ErrorCode.INVALID_CREDENTIALS)
        }

        // A success clears the counter, so a bad evening does not throttle the
        // household for the rest of it. A failure here is a stale row, not a
        // reason to refuse a session the password already earned.
        try {
            loginAttemptRepository.deleteByKeyIn(keys)
        } catch (e: RuntimeException) {
            if (!e.isDatabaseFailure()) throw e
            log.warn("login: clear attempts", e)
        }

        // Read after the permit wait, which can run to HTTP_WRITE_TIMEOUT: read
        // before it, a queued login's session would expire that much early.
        // Go reads its clock when it issues the session too.
        val now = clock.instant()
        val (token, hash) = TokenService.issue()
        val expiresAt = now.plus(appConfig.sessionTtl)
        try {
            // Flushed here rather than at commit, so a failed insert is caught
            // and logged as "issue session" instead of surfacing bare from the
            // transaction boundary.
            sessionRepository.saveAndFlush(
                Session(
                    sessionId = hash,
                    userId = user.id,
                    createdAt = now,
                    expiresAt = expiresAt,
                    lastSeenAt = now,
                    userAgent = RequestContext.current()?.userAgent
                )
            )
        } catch (e: RuntimeException) {
            if (!e.isDatabaseFailure()) throw e
            log.error("login: issue session", e)
            throw ApiException(500, ErrorCode.INTERNAL)
        }
        return Issue(sessionCookieFactory.set(token, expiresAt))
    }

    private fun refuseWhileThrottled(keys: List<String>) {
        val remaining = try {
            loginAttemptRepository.activeBackoffSeconds(keys)
        } catch (e: RuntimeException) {
            if (!e.isDatabaseFailure()) throw e
            log.error("login: read backoff", e)
            throw ApiException(500, ErrorCode.INTERNAL)
        }
        if (remaining != null) {
            // Rounded up: Retry-After of 0 invites an immediate retry inside the window.
            val retryAfter = remaining.toInt() + 1
            throw ApiException(429, ErrorCode.TOO_MANY_ATTEMPTS, retryAfterSeconds = retryAfter)
        }
    }

    // The hash to check the password against, and the User it belongs to. With
    // no credential it is the dummy hash and no User: a dormant User (invited,
    // never set a password) or an unknown address costs the same work as a
    // real one, so timing cannot enumerate accounts either.
    //
    // A lookup that failed says nothing about this account's credentials: 500,
    // not 401, thrown before the permit queue and recordFailures, so an outage
    // burns no backoff (#23).
    private fun resolve(normalizedEmail: String): Pair<User?, String> {
        val user = lookUp("login: look up user") { userRepository.findByEmail(normalizedEmail) }
            ?: return null to PasswordService.dummyHash
        val hash = lookUp("login: look up credential") { credentialRepository.findByUserId(user.id) }
            ?.passwordHash
            ?: return null to PasswordService.dummyHash
        return user to hash
    }

    // One of resolve's lookups, logged under its own name so the log says which
    // failed, as Go's wrapped error does.
    private inline fun <T> lookUp(what: String, query: () -> T): T = try {
        query()
    } catch (e: RuntimeException) {
        if (!e.isDatabaseFailure()) throw e
        log.error(what, e)
        throw ApiException(500, ErrorCode.INTERNAL)
    }

    // Holds one Argon2 permit from a second backoff read until any failure is
    // recorded. The first read, before the queue, sees none of the failures of
    // the logins queued alongside it: a burst on one account all passed it, all
    // queued, and all had their passwords checked, the backoff never applying
    // (#33). Read under the permit instead, and written before the permit
    // passes on, one caller's failure throttles the next. At most the cap's
    // worth of callers are checked at once, so a burst gets that many guesses,
    // not the whole queue.
    private fun checkPassword(keys: List<String>, password: String, phc: String): Boolean = try {
        passwordService.withPermit {
            refuseWhileThrottled(keys)
            passwordService.verifyHoldingPermit(password, phc).also { if (!it) recordFailures(keys) }
        }
    } catch (e: HashingUnavailableException) {
        // Queued past the wait for an Argon2 permit (#33): the password was
        // never checked, so this is neither a 401 nor a backoff failure.
        log.error("login: wait for argon2 permit", e)
        throw ApiException(500, ErrorCode.INTERNAL)
    }

    // A counter that cannot be written is logged, never raised: a 500 here
    // would answer a wrong password differently from a right one.
    private fun recordFailures(keys: List<String>) {
        keys.forEach {
            try {
                loginAttemptRepository.recordFailure(it, FIRST_BACKOFF_SECONDS, MAX_BACKOFF_SECONDS)
            } catch (e: RuntimeException) {
                if (!e.isDatabaseFailure()) throw e
                log.error("login: record failure", e)
            }
        }
    }

    // Not @Transactional, like login: the transaction would begin at the proxy,
    // before this body, so an outage would fail there and never reach the catch
    // below. deleteRow is its own transaction instead.
    fun logout(): String {
        // Revocation is the row delete. Deleting an absent row is a no-op, so a
        // session-less logout stays idempotent.
        val token = RequestContext.current()?.sessionToken
        if (!token.isNullOrBlank()) {
            try {
                sessionRepository.deleteRow(TokenService.hash(token))
            } catch (e: RuntimeException) {
                if (!e.isDatabaseFailure()) throw e
                log.error("logout: delete session", e)
                throw ApiException(500, ErrorCode.INTERNAL)
            }
        }
        return sessionCookieFactory.clear()
    }

    @Transactional(readOnly = true)
    fun me(userId: UUID): Me {
        val user = userRepository.findById(userId).orElse(null)
            ?: throw ApiException(401, ErrorCode.UNAUTHORIZED)
        // Scoped by the session's own household: read from the authenticated
        // User, never from anything the request supplied.
        val household = householdRepository.findById(user.householdId).orElse(null)
        if (household == null) {
            // A User whose Household is gone cannot be served coherently.
            log.error("me: household missing for user, household_id={}", user.householdId)
            throw ApiException(500, ErrorCode.INTERNAL)
        }
        return Me(
            id = user.id,
            householdId = user.householdId,
            email = user.email,
            displayName = user.displayName,
            locale = Locale.forValue(user.locale),
            timeZone = user.timeZone,
            householdDisplayName = household.displayName,
            reportingCurrency = household.reportingCurrency,
            periodStartDay = household.periodStartDay,
            dayStartsAt = formatDayStartsAt(household.dayStartsAt),
            allowanceMode = Me.AllowanceMode.forValue(household.allowanceMode),
            expectedMonthlyIncome = household.expectedMonthlyIncome?.toPlainString(),
        )
    }

    private fun formatDayStartsAt(time: LocalTime): String =
        time.format(TIME_FORMATTER)

    // Go's trim set (login.go's normalizeEmail), so a padded address finds the
    // same row in both backends.
    private fun normalizeEmail(email: String): String = email.trimSpace().lowercase()

    private fun backoffKeys(email: String): List<String> {
        val keys = mutableListOf("email:$email")
        val ip = RequestContext.current()?.clientIp
        if (!ip.isNullOrBlank()) keys += "ip:$ip"
        return keys
    }

    companion object {
        // Backoff shape: doubling from one second, capped at five minutes.
        const val FIRST_BACKOFF_SECONDS = 1L
        const val MAX_BACKOFF_SECONDS = 300L

        private val TIME_FORMATTER: DateTimeFormatter = DateTimeFormatter.ofPattern("HH:mm")
        private val log = LoggerFactory.getLogger(AuthService::class.java)
    }
}
