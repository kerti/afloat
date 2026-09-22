package dev.kerti.afloat.auth

import dev.kerti.afloat.api.model.ErrorCode
import dev.kerti.afloat.httperr.ApiErrorWriter
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.springframework.security.access.AccessDeniedException
import org.springframework.security.web.access.AccessDeniedHandler

// The other half of #14 §3.5: Spring Security writes BOTH the unauthenticated
// and the access-denied response from a filter, outside controller exception
// handling, so a @ControllerAdvice covers neither. The entry point was wired;
// this was not, and its default serves an HTML error page.
//
// It answers 401 UNAUTHORIZED, not a 403. Two reasons, and neither is laziness:
//
//   - The contract's ErrorCode enum is a closed set with no FORBIDDEN in it
//     (contract/openapi.yaml). Emitting a code the contract does not declare is
//     the bug CLAUDE.md names; inventing one here would be resolving a contract
//     question by writing code that assumes the answer.
//   - Go has no authorization tier at all — RequireAuth writes 401 UNAUTHORIZED
//     and there is nothing between "has a session" and "may proceed"
//     (backend/internal/auth/session.go). So 401 is what the other backend
//     answers for every denial there is, and parity is the point.
//
// Unreachable today: the chain carries no authorities, so ExceptionTranslationFilter
// routes every anonymous denial to the entry point instead. When step 9 brings a
// real authorization rule, a FORBIDDEN code in the contract is the prerequisite,
// and this class is where it lands.
class EnvelopeAccessDeniedHandler : AccessDeniedHandler {
    override fun handle(
        request: HttpServletRequest,
        response: HttpServletResponse,
        accessDeniedException: AccessDeniedException,
    ) {
        ApiErrorWriter.write(response, 401, ErrorCode.UNAUTHORIZED)
    }
}
