package dev.kerti.afloat

import org.springframework.boot.autoconfigure.SpringBootApplication
import org.springframework.boot.runApplication

@SpringBootApplication
class AfloatApplication

fun main(args: Array<String>) {
	runApplication<AfloatApplication>(*args)
}
