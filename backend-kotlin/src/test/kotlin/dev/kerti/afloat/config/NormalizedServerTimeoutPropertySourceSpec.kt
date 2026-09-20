package dev.kerti.afloat.config

import io.kotest.assertions.throwables.shouldThrow
import io.kotest.core.spec.style.StringSpec
import io.kotest.datatest.withData
import io.kotest.matchers.nulls.shouldBeNull
import io.kotest.matchers.shouldBe
import org.springframework.boot.context.properties.source.ConfigurationPropertySources
import org.springframework.boot.context.properties.bind.Binder
import org.springframework.core.env.MapPropertySource
import org.springframework.core.env.PropertySource
import org.springframework.mock.env.MockEnvironment
import java.time.Duration

data class TimeoutRelayCase(
    val name: String,
    val leaf: String,
    val raw: String,
    val property: String,
    val expected: String,
)

open class NormalizedServerTimeoutPropertySourceSpec : StringSpec({

    withData(
        nameFn = { it.name },
        listOf(
            TimeoutRelayCase("read single", "afloat.http-read-timeout", "30s", "server.tomcat.connection-timeout", "PT30S"),
            TimeoutRelayCase("read compound", "afloat.http-read-timeout", "1m30s", "server.tomcat.connection-timeout", "PT1M30S"),
            TimeoutRelayCase("read fractional", "afloat.http-read-timeout", "1.5h", "server.tomcat.connection-timeout", "PT1H30M"),
            TimeoutRelayCase("idle compound", "afloat.http-idle-timeout", "90m0s", "server.tomcat.keep-alive-timeout", "PT1H30M"),
            TimeoutRelayCase("shutdown", "afloat.shutdown-timeout", "10s", "spring.lifecycle.timeout-per-shutdown-phase", "PT10S"),
        )
    ) { (_, leaf, raw, property, expected) ->
        val source = NormalizedServerTimeoutPropertySource(MockEnvironment().withProperty(leaf, raw))
        source.getProperty(property) shouldBe expected
    }

    "absent leaves leave the relayed properties unresolved" {
        val source = NormalizedServerTimeoutPropertySource(MockEnvironment())
        source.getProperty("server.tomcat.connection-timeout").shouldBeNull()
        source.getProperty("server.tomcat.keep-alive-timeout").shouldBeNull()
        source.getProperty("spring.lifecycle.timeout-per-shutdown-phase").shouldBeNull()
    }

    "resolves nothing but the three relayed properties" {
        val source = NormalizedServerTimeoutPropertySource(
            MockEnvironment().withProperty("afloat.http-read-timeout", "1m30s")
        )
        source.getProperty("afloat.http-read-timeout").shouldBeNull()
        source.getProperty("HTTP_READ_TIMEOUT").shouldBeNull()
        source.getProperty("server.port").shouldBeNull()
    }

    "invalid duration fails with the AppConfig message" {
        val source = NormalizedServerTimeoutPropertySource(
            MockEnvironment().withProperty("afloat.http-read-timeout", "1.5x")
        )
        val ex = shouldThrow<ConfigException> { source.getProperty("server.tomcat.connection-timeout") }
        ex.message shouldBe "HTTP_READ_TIMEOUT: '1.5x' is not a valid duration"
    }

    "the relayed ISO-8601 binds through Spring's binder as the parsed Duration" {
        val env = org.springframework.core.env.StandardEnvironment()
        env.propertySources.addFirst(
            MapPropertySource("test-leaf", mapOf("afloat.http-read-timeout" to "1m30s"))
        )
        env.propertySources.addFirst(
            NormalizedServerTimeoutPropertySource(env) as PropertySource<*>
        )
        ConfigurationPropertySources.attach(env)
        val bound = Binder.get(env).bind("server.tomcat.connection-timeout", Duration::class.java)
        bound.orElse(null) shouldBe Duration.ofMinutes(1).plusSeconds(30)
    }
})
