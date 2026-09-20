package dev.kerti.afloat

import dev.kerti.afloat.config.ConfigPreflightInitializer
import org.springframework.boot.autoconfigure.SpringBootApplication
import org.springframework.boot.runApplication

@SpringBootApplication
class AfloatApplication

fun main(args: Array<String>) {
    // Preflight is an initializer added here rather than a System.getenv() call
    // in this function: by the time an initializer runs the Environment is fully
    // resolved (env, command-line args, spring.config.import) - the surface an
    // operator actually configures. AppConfiguration's bean then parses again
    // inside the context. Both are deliberate; this one prints and exits 1, the
    // bean is the injectable one.
    runApplication<AfloatApplication>(*args) {
        addInitializers(ConfigPreflightInitializer())
    }
}
