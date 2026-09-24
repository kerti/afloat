package dev.kerti.afloat.security

import dev.kerti.afloat.api.SystemApi
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import io.kotest.matchers.shouldBe
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get

class SecurityHeadersSpec : WebDatabaseSpec() {

    init {
        "sends the six pinned security headers on every response" {
            val response = mockMvc.perform(
                get(SystemApi.BASE_PATH + SystemApi.PATH_GET_HEALTH)
            ).andReturn().response

            val want = mapOf(
                "X-Content-Type-Options" to "nosniff",
                "X-Frame-Options" to "DENY",
                "Cache-Control" to "no-cache, no-store, max-age=0, must-revalidate",
                "Pragma" to "no-cache",
                "Expires" to "0",
                "X-XSS-Protection" to "0",
            )
            want.forEach { (name, value) ->
                response.getHeader(name) shouldBe value
            }
        }

        "never sends Strict-Transport-Security, even on a secure request" {
            val response = mockMvc.perform(
                get(
                    SystemApi.BASE_PATH + SystemApi.PATH_GET_HEALTH
                ).secure(true)
            ).andReturn().response

            response.getHeader("Strict-Transport-Security") shouldBe null
        }
    }
}
