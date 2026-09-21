package dev.kerti.afloat.httperr

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.api.SystemApi
import dev.kerti.afloat.api.model.ErrorCode
import dev.kerti.afloat.testsupport.WebDatabaseSpec
import io.kotest.assertions.withClue
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldNotContain
import org.springframework.test.web.servlet.MvcResult
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.delete
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post

// #13 tests 22-24. Spring Boot's default error response is served from a path
// most exception handling never sees — the container's ERROR dispatch, not a
// controller — and it carries timestamp, status, error, path and sometimes
// message. Every one of those is a field the contract forbids, and `message`
// is the one that breaks non-negotiable 7 outright.
class ErrorEnvelopeSpec : WebDatabaseSpec() {

    // The keys Boot's DefaultErrorAttributes writes. None may appear anywhere.
    private val springDefaultKeys = listOf("timestamp", "\"status\"", "\"error\"", "\"path\"", "message", "trace")

    private fun MvcResult.carriesNoSpringDefault() {
        val body = response.contentAsString
        springDefaultKeys.forEach { key ->
            withClue("body was: $body") { body shouldNotContain key }
        }
    }

    init {
        // unknownRouteReturnsContractErrorEnvelope
        //
        // 401, not 404, and that is a DIVERGENCE from Go, pinned here rather
        // than left to be found. Authorization runs in the filter chain, before
        // routing, so an unknown path is refused as unauthenticated before
        // anything discovers there is no handler — a consequence of "public by
        // exception, authenticated by default" (SecurityConfiguration).
        //
        // Go mounts its routes on chi and answers 404
        // (httpserver/server_test.go asserts exactly that for /api/nope).
        // Neither is wrong: Kotlin's declines to confirm which routes exist,
        // Go's is the ordinary thing a router does. They disagree, and choosing
        // for them here would bake the answer into a test, so it is recorded
        // as-is and raised on #14.
        //
        // What is NOT in doubt, and is the point of #13 test 22: the body is
        // the contract envelope, never Boot's default error page.
        "answers an unknown route with the envelope, never Boot's error body" {
            listOf("/api/nope", "/api/auth/nope", "/nope").forEach { path ->
                val result = mockMvc.perform(get(path)).andReturn()
                withClue(path) {
                    result.response.status shouldBe 401
                    result.response.contentAsString shouldBe """{"code":"UNAUTHORIZED"}"""
                    result.carriesNoSpringDefault()
                }
            }
        }

        "answers a wrong method with a bare 405 and none of Boot's error body" {
            val result = mockMvc.perform(delete(SystemApi.BASE_PATH + SystemApi.PATH_GET_HEALTH)).andReturn()

            result.response.status shouldBe 405
            result.carriesNoSpringDefault()
        }

        // errorEnvelopeCarriesNoMessageField
        //
        // Asserted on a response that really is an envelope, not only on the
        // empty ones above: the absence has to hold where there IS a body.
        "carries no message field on a real envelope" {
            val result = mockMvc.perform(get(AuthApi.BASE_PATH + AuthApi.PATH_GET_ME)).andReturn()

            result.response.status shouldBe 401
            result.response.contentAsString shouldBe """{"code":"UNAUTHORIZED"}"""
            result.carriesNoSpringDefault()
        }

        // The envelope written from a FILTER rather than a controller — the
        // path that has no @ControllerAdvice over it at all.
        "carries no message field on an envelope written by a filter" {
            val result = mockMvc.perform(
                post(AuthApi.BASE_PATH + AuthApi.PATH_LOCAL_LOGIN).header("Sec-Fetch-Site", "cross-site"),
            ).andReturn()

            result.response.status shouldBe 403
            result.response.contentAsString shouldBe """{"code":"CROSS_SITE_REQUEST_BLOCKED"}"""
            result.carriesNoSpringDefault()
        }

        // unhandledExceptionReturns500WithInternalCodeOnly
        //
        // Through the advice directly: an exception nothing anticipated cannot
        // be provoked through the wire without breaking a dependency, and the
        // guarantee being asserted is about the shape, not about the route.
        "reduces an unhandled exception to INTERNAL and nothing else" {
            val response = ApiExceptionHandler().handleUnexpected(
                IllegalStateException("connection to 10.0.0.4 refused for user afloat"),
            )

            response.statusCode.value() shouldBe 500
            response.body?.code shouldBe ErrorCode.INTERNAL
            // The cause is logged where it happened; the client is told nothing
            // about it. An args map here would leak the message the code exists
            // to replace.
            response.body?.args shouldBe null
        }
    }
}
