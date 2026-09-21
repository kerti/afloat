package dev.kerti.afloat.security

import dev.kerti.afloat.auth.CrossSiteGuardFilter
import dev.kerti.afloat.auth.MaxBodyFilter
import dev.kerti.afloat.auth.RequestFactsFilter
import dev.kerti.afloat.auth.SessionFilter
import dev.kerti.afloat.testsupport.DatabaseSpec
import io.kotest.matchers.ints.shouldBeGreaterThanOrEqual
import io.kotest.matchers.ints.shouldBeLessThan
import jakarta.servlet.Filter
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.security.web.FilterChainProxy

class SecurityFilterOrderSpec : DatabaseSpec() {

    @Autowired
    private lateinit var filterChainProxy: FilterChainProxy

    init {
        // Go's chain: maxBodyBytes -> crossSiteGuard -> RequestContextMiddleware
        // -> SessionMiddleware (server.go:75-86). The session filter reads the
        // facts the request-facts filter sets, so the order is load-bearing,
        // not cosmetic.
        "runs the request filters in Go's order" {
            val filters = filterChainProxy.filterChains.first().filters
            val maxBody = filters.indexOfFilter<MaxBodyFilter>()
            val crossSite = filters.indexOfFilter<CrossSiteGuardFilter>()
            val facts = filters.indexOfFilter<RequestFactsFilter>()
            val session = filters.indexOfFilter<SessionFilter>()

            // indexOfFirst returns -1 for a filter that was never registered,
            // which would otherwise satisfy every ordering assertion below.
            maxBody shouldBeGreaterThanOrEqual 0
            crossSite shouldBeGreaterThanOrEqual 0
            facts shouldBeGreaterThanOrEqual 0
            session shouldBeGreaterThanOrEqual 0

            maxBody shouldBeLessThan crossSite
            crossSite shouldBeLessThan facts
            facts shouldBeLessThan session
        }
    }
}

private inline fun <reified T : Filter> List<Filter>.indexOfFilter(): Int = indexOfFirst { it is T }
