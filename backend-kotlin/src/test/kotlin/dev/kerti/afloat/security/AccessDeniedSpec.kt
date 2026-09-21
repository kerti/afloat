package dev.kerti.afloat.security

import dev.kerti.afloat.auth.EnvelopeAccessDeniedHandler
import dev.kerti.afloat.httperr.ApiExceptionHandler
import io.kotest.assertions.throwables.shouldThrow
import io.kotest.core.spec.style.StringSpec
import io.kotest.matchers.shouldBe
import io.kotest.matchers.types.shouldBeSameInstanceAs
import io.kotest.matchers.string.shouldNotContain
import org.springframework.mock.web.MockHttpServletRequest
import org.springframework.mock.web.MockHttpServletResponse
import org.springframework.security.access.AccessDeniedException

// #14 §3.5 asks for BOTH filter-written responses to carry the envelope. The
// entry point had a test through /me; the access-denied handler had neither a
// wiring nor a test, and its default serves an HTML error page.
//
// Unreachable through the chain today — nothing carries an authority, so every
// anonymous denial goes to the entry point instead — so it is asserted
// directly. A test that could only run once roles exist would mean the default
// stayed in place until then.
class AccessDeniedSpec : StringSpec({

    "writes the contract envelope rather than Spring's default page" {
        val response = MockHttpServletResponse()

        EnvelopeAccessDeniedHandler().handle(
            MockHttpServletRequest("GET", "/api/me"),
            response,
            AccessDeniedException("Access is denied"),
        )

        // 401 UNAUTHORIZED: the contract's ErrorCode enum is closed and carries
        // no FORBIDDEN, and Go has no authorization tier at all — every denial
        // there is RequireAuth's 401. See EnvelopeAccessDeniedHandler.
        response.status shouldBe 401
        response.contentAsString shouldBe """{"code":"UNAUTHORIZED"}"""
        response.contentType shouldBe "application/json;charset=UTF-8"
        response.contentAsString shouldNotContain "message"
        response.contentAsString shouldNotContain "Access is denied"
    }

    // The handler above can only ever run if the exception REACHES
    // ExceptionTranslationFilter. ApiExceptionHandler's @ExceptionHandler(
    // Exception::class) catch-all sits inside the handler, further in, and
    // would answer 500 INTERNAL first — leaving the handler this spec just
    // asserted unreachable for a second reason once step 9's authorization
    // rules make the first one (no authorities on the chain) go away.
    //
    // Rethrowing the SAME exception is how a @ControllerAdvice declines:
    // ExceptionHandlerExceptionResolver reads it as "not handled" and lets the
    // original continue up the chain.
    "declines AccessDeniedException so the filter chain still sees it" {
        val denial = AccessDeniedException("Access is denied")

        val rethrown = shouldThrow<AccessDeniedException> {
            ApiExceptionHandler().rethrowAccessDenied(denial)
        }

        // The same instance, not a copy: a different one would be logged as an
        // accident and the resolver's contract would not apply.
        rethrown shouldBeSameInstanceAs denial
    }
})
