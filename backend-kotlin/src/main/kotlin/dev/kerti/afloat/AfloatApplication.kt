package dev.kerti.afloat

import dev.kerti.afloat.config.AppConfig
import dev.kerti.afloat.config.ConfigException
import org.springframework.boot.autoconfigure.SpringBootApplication
import org.springframework.boot.runApplication
import kotlin.system.exitProcess

@SpringBootApplication
class AfloatApplication

fun main(args: Array<String>) {
    // Loaded here for an operator-legible failure before Spring starts, and again
    // by AppConfiguration's bean inside the context. Both are deliberate. This one
    // prints and exits 1, the bean is the injectable one.
    try {
        AppConfig.load(System.getenv())
    } catch (e: ConfigException) {
        System.err.println(e.message)
        exitProcess(1)
    }
    runApplication<AfloatApplication>(*args)
}
