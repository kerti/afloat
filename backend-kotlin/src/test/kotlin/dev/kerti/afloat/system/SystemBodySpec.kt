package dev.kerti.afloat.system

import dev.kerti.afloat.api.SystemApi
import dev.kerti.afloat.config.AppConfig
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import dev.kerti.afloat.testsupport.jsonFields
import io.kotest.assertions.withClue
import io.kotest.matchers.shouldBe
import org.mockito.Mockito.`when`
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.test.context.bean.override.mockito.MockitoBean
import org.springframework.test.web.servlet.MvcResult
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get

// #13 tests 8-9, 11, 13-15 and 17, on the WIRE.
//
// SystemControllerSpec calls the controller directly and compares
// ResponseEntity.body to a Health(...) object, which is object equality: it
// cannot see what Jackson emits, so `additionalProperties: false` was asserted
// nowhere. That is the one property a hand-written DTO, a Jackson module or a
// generator change can break without touching this application's code, and
// 6a's definition of done — "identical bodies modulo version" against the Go
// backend — is a statement about these bytes.
//
// MeSpec already reads its body this way; the two system routes are the gap.
class SystemBodySpec : WebDatabaseSpec() {

    @Autowired
    private lateinit var appConfig: AppConfig

    // The probe is the only seam between "reachable" and "degraded" that does
    // not require taking the container away from every other spec in the JVM.
    @MockitoBean
    private lateinit var probe: DatabaseProbe

    private fun healthy(reachable: Boolean) {
        `when`(probe.isReachable()).thenReturn(reachable)
    }

    private fun getPath(path: String): MvcResult = mockMvc.perform(get(path)).andReturn()

    init {
        // healthReturns200AndOkWhenDatabaseReachable
        // healthBodyHasNoAdditionalProperties
        // healthVersionReflectsConfiguration
        "answers health with exactly status and version" {
            healthy(true)

            val result = getPath(SystemApi.BASE_PATH + SystemApi.PATH_GET_HEALTH)

            result.response.status shouldBe 200
            // The key SET, not merely the presence of the two: Health declares
            // additionalProperties: false, and Jackson will happily add a field
            // the contract forbids the day something carries one.
            result.response.contentAsString.jsonFields() shouldBe mapOf(
                "status" to "\"ok\"",
                "version" to "\"${appConfig.version}\"",
            )
        }

        // healthReturns503AndDegradedWhenDatabaseUnreachable
        "answers a degraded health with 503 and the same two fields" {
            healthy(false)

            val result = getPath(SystemApi.BASE_PATH + SystemApi.PATH_GET_HEALTH)

            // A database that is down is a reportable state, not an error: the
            // body is the same shape whether or not the instance is well, never
            // an error envelope. Go says the same (system.go).
            result.response.status shouldBe 503
            result.response.contentAsString.jsonFields() shouldBe mapOf(
                "status" to "\"degraded\"",
                "version" to "\"${appConfig.version}\"",
            )
        }

        // authMethodsReflectsConfiguredProviders
        // authMethodsBodyHasNoAdditionalProperties
        "answers auth methods with exactly local and google" {
            healthy(true)

            val result = getPath(SystemApi.BASE_PATH + SystemApi.PATH_GET_AUTH_METHODS)

            result.response.status shouldBe 200
            // Booleans unquoted: the contract declares them boolean, and the
            // string-serialisation rule is money's alone (BOOTSTRAP §4).
            result.response.contentAsString.jsonFields() shouldBe mapOf(
                "local" to appConfig.authLocalEnabled.toString(),
                "google" to appConfig.authGoogleEnabled.toString(),
            )
        }

        // healthIsReachableWithoutAuthentication
        // authMethodsIsReachableWithoutAuthentication
        //
        // SecurityChainSpec asserts the status; this asserts that the BODY is
        // the contract's and not an envelope, because a route that is public
        // but answers a 401 body would still read as 200 there.
        "serves both bodies with no session and a garbage cookie present" {
            healthy(true)

            listOf(
                SystemApi.BASE_PATH + SystemApi.PATH_GET_HEALTH,
                SystemApi.BASE_PATH + SystemApi.PATH_GET_AUTH_METHODS,
            ).forEach { path ->
                withClue(path) {
                    val result = mockMvc.perform(
                        get(path).cookie(
                            jakarta.servlet.http.Cookie("afloat_session", "not a token that was ever issued")
                        )
                    ).andReturn()

                    result.response.status shouldBe 200
                    result.response.contentAsString.jsonFields().keys shouldBe
                        if (path.endsWith("health")) setOf("status", "version") else setOf("local", "google")
                }
            }
        }
    }
}
