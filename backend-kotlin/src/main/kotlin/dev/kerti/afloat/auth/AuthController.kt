package dev.kerti.afloat.auth

import dev.kerti.afloat.api.AuthApi
import dev.kerti.afloat.api.model.ErrorCode
import dev.kerti.afloat.api.model.LocalLoginRequest
import dev.kerti.afloat.api.model.Me
import dev.kerti.afloat.httperr.ApiException
import org.springframework.http.HttpHeaders
import org.springframework.http.ResponseEntity
import org.springframework.security.core.context.SecurityContextHolder
import org.springframework.web.bind.annotation.RestController

@RestController
class AuthController(
    private val authService: AuthService,
) : AuthApi {
    override fun localLogin(localLoginRequest: LocalLoginRequest): ResponseEntity<Unit> {
        val issued = authService.login(localLoginRequest.email, localLoginRequest.password)
        return ResponseEntity.noContent()
            .header(HttpHeaders.SET_COOKIE, issued.cookie)
            .build()
    }

    override fun logout(): ResponseEntity<Unit> {
        // The service clears the cookie unconditionally, including for a
        // request that presented no session: the point is that the client
        // stops holding a token.
        val cleared = authService.logout()
        return ResponseEntity.noContent()
            .header(HttpHeaders.SET_COOKIE, cleared)
            .build()
    }

    override fun getMe(): ResponseEntity<Me> {
        // Unreachable while /me is an authenticated route, but the handler does
        // not depend on the chain being configured that way.
        val principal = SecurityContextHolder.getContext().authentication?.principal as? AuthenticatedUser
            ?: throw ApiException(401, ErrorCode.UNAUTHORIZED)
        return ResponseEntity.ok(authService.me(principal.userId))
    }
}
