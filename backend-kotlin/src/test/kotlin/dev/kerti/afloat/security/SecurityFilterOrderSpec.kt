package dev.kerti.afloat.security

import dev.kerti.afloat.auth.CrossSiteGuardFilter
import dev.kerti.afloat.auth.DisabledLocalLogin404Filter
import dev.kerti.afloat.auth.MaxBodyFilter
import dev.kerti.afloat.auth.RequestFactsFilter
import dev.kerti.afloat.auth.SessionFilter
import dev.kerti.afloat.testsupport.DatabaseSpec
import io.kotest.matchers.ints.shouldBeGreaterThanOrEqual
import io.kotest.matchers.ints.shouldBeLessThan
import io.kotest.matchers.shouldBe
import jakarta.servlet.Filter
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.security.web.FilterChainProxy
import org.springframework.security.web.authentication.UsernamePasswordAuthenticationFilter
import org.springframework.security.web.authentication.www.BasicAuthenticationFilter
import org.springframework.security.web.csrf.CsrfFilter

class SecurityFilterOrderSpec : DatabaseSpec() {

    @Autowired
    private lateinit var filterChainProxy: FilterChainProxy

    init {
        // Go's chain: disabledLocalLogin404 -> maxBodyBytes -> crossSiteGuard
        // -> RequestContextMiddleware -> SessionMiddleware (server.go:75-86).
        // The session filter reads the facts the request-facts filter sets, so
        // the order is load-bearing, not cosmetic.
        "runs the request filters in Go's order" {
            val filters = filterChainProxy.filterChains.first().filters
            val localLogin404 = filters.indexOfFilter<DisabledLocalLogin404Filter>()
            val maxBody = filters.indexOfFilter<MaxBodyFilter>()
            val crossSite = filters.indexOfFilter<CrossSiteGuardFilter>()
            val facts = filters.indexOfFilter<RequestFactsFilter>()
            val session = filters.indexOfFilter<SessionFilter>()

            // indexOfFirst returns -1 for a filter that was never registered,
            // which would otherwise satisfy every ordering assertion below.
            localLogin404 shouldBeGreaterThanOrEqual 0
            maxBody shouldBeGreaterThanOrEqual 0
            crossSite shouldBeGreaterThanOrEqual 0
            facts shouldBeGreaterThanOrEqual 0
            session shouldBeGreaterThanOrEqual 0

            localLogin404 shouldBeLessThan maxBody
            maxBody shouldBeLessThan crossSite
            crossSite shouldBeLessThan facts
            facts shouldBeLessThan session
        }

        // csrfIsDisabledInSpringSecurity (#13 test 21)
        //
        // Asserted by absence from the real chain, not by reading `.csrf {
        // it.disable() }` back out of the configuration. Spring's token CSRF is
        // not the design: the defence is SameSite=Lax plus CrossSiteGuardFilter
        // (BOOTSTRAP §5.1), and CsrfFilter alongside it would reject every POST
        // that carries no token — every login, from every client that is not
        // the browser app.
        "mounts no CsrfFilter" {
            val filters = filterChainProxy.filterChains.first().filters

            filters.indexOfFilter<CsrfFilter>() shouldBe -1
            // The guard that replaces it is present, so this cannot pass by
            // there being no chain at all.
            filters.indexOfFilter<CrossSiteGuardFilter>() shouldBeGreaterThanOrEqual 0
        }

        // httpBasicAndFormLoginAreDisabled (#13 test 19)
        //
        // The header half is asserted through /me in SecurityChainSpec; this is
        // the other half — neither filter is mounted, so there is no /login
        // route and no generated form for anything to be redirected to.
        "mounts neither form login nor HTTP Basic" {
            val filters = filterChainProxy.filterChains.first().filters

            filters.indexOfFilter<UsernamePasswordAuthenticationFilter>() shouldBe -1
            filters.indexOfFilter<BasicAuthenticationFilter>() shouldBe -1
        }
    }
}

private inline fun <reified T : Filter> List<Filter>.indexOfFilter(): Int = indexOfFirst { it is T }
