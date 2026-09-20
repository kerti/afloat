package dev.kerti.afloat.config

import org.springframework.boot.jdbc.DataSourceBuilder
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import org.springframework.core.env.Environment
import javax.sql.DataSource

@Configuration
class AppConfiguration {

    @Bean
    fun appConfig(env: Environment): AppConfig = AppConfig.from(env)

    // The connection layer AppConfig.parse()/validate() deliberately leave to later.
    // The libpq DATABASE_URL becomes the JDBC DataSource that Flyway and JPA share.
    @Bean
    fun dataSource(appConfig: AppConfig): DataSource {
        val source = PostgresUrlTranslator.translate(appConfig.databaseUrl)
        val builder = DataSourceBuilder.create().url(source.jdbcUrl)

        // Conditional, a URL with no credentials must leave the driver's own default
        // in place, exactly as libpq would.
        if (source.username != null) builder.username(source.username)
        if (source.password != null) builder.password(source.password)

        return builder.build()
    }
}
