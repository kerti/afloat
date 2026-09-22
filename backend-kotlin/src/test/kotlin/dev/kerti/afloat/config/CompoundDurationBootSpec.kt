package dev.kerti.afloat.config

import dev.kerti.afloat.testsupport.CompoundDurationDatabaseInitializer
import dev.kerti.afloat.testsupport.SpringDatabaseSpec
import io.kotest.matchers.shouldBe
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.boot.autoconfigure.context.LifecycleProperties
import org.springframework.boot.tomcat.autoconfigure.TomcatServerProperties
import org.springframework.test.context.ContextConfiguration
import java.time.Duration

// The half NormalizedServerTimeoutPropertySourceSpec cannot reach: that spec
// proves ISO-8601 binds through Boot's Binder, holding the property source in
// its own hand. This proves the source is actually REGISTERED - spring.factories
// -> ServerTimeoutNormalizingContextInitializer -> addFirst - and that a spelling
// Go accepts survives a real boot into the properties Boot binds.
//
// That the context loads at all is half the assertion: a raw 1m30s reaching
// Boot's simple duration style fails the context, which is exactly what this
// backend did before the normalizer landed.
//
// It stops at the bound properties on purpose. Tomcat's own consumption of
// TomcatServerProperties is Boot's job; reaching into the connector would be testing
// Spring Boot rather than this repo.
@ContextConfiguration(initializers = [CompoundDurationDatabaseInitializer::class])
class CompoundDurationBootSpec : SpringDatabaseSpec() {

    @Autowired
    private lateinit var tomcatProperties: TomcatServerProperties

    @Autowired
    private lateinit var lifecycleProperties: LifecycleProperties

    init {
        "a compound HTTP_READ_TIMEOUT binds as the parsed duration" {
            tomcatProperties.connectionTimeout shouldBe Duration.ofSeconds(90)
        }

        "a compound HTTP_IDLE_TIMEOUT binds as the parsed duration" {
            tomcatProperties.keepAliveTimeout shouldBe Duration.ofMinutes(90)
        }

        "a fractional SHUTDOWN_TIMEOUT binds as the parsed duration" {
            lifecycleProperties.timeoutPerShutdownPhase shouldBe Duration.ofSeconds(90)
        }
    }
}
