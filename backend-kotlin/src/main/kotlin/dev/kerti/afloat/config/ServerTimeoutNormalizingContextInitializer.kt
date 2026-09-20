package dev.kerti.afloat.config

import org.springframework.context.ApplicationContextInitializer
import org.springframework.context.ConfigurableApplicationContext

// Registered in META-INF/spring.factories so it runs for both the packaged app
// and @SpringBootTest contexts, before Boot binds server.tomcat.* and
// spring.lifecycle.*. Pairs with NormalizedServerTimeoutPropertySource the way
// FlywayEnablementContextInitializer pairs with AutoMigrateFlywayPropertySource.
class ServerTimeoutNormalizingContextInitializer : ApplicationContextInitializer<ConfigurableApplicationContext> {
    override fun initialize(context: ConfigurableApplicationContext) {
        context.environment.propertySources.addFirst(
            NormalizedServerTimeoutPropertySource(context.environment)
        )
    }
}
