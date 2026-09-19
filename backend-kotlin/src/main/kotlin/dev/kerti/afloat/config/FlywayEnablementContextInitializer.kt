package dev.kerti.afloat.config

import org.springframework.context.ApplicationContextInitializer
import org.springframework.context.ConfigurableApplicationContext

// Registered in META-INF/spring.factories so it runs for both the packaged app and
// @SpringBootTest contexts. An initializer is the hook that lands before the
// Flyway auto-configuration condition is evaluated; a @Bean cannot, because
// conditions are resolved during configuration parsing, before beans are made.
// FlywayEnablementContextInitializer pairs with AppConfiguration's AppConfig bean:
// the bean owns the parsed value, this owns getting that value onto the property
// Flyway actually conditions on.
class FlywayEnablementContextInitializer : ApplicationContextInitializer<ConfigurableApplicationContext> {
    override fun initialize(context: ConfigurableApplicationContext) {
        context.environment.propertySources.addFirst(
            AutoMigrateFlywayPropertySource(context.environment)
        )
    }
}