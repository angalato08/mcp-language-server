#include <iostream>
#include <string>

std::string leafFunction() {
    return "leaf result";
}

std::string middleFunction() {
    std::string result = leafFunction();
    return "middle: " + result;
}

void entryPoint() {
    std::string msg = middleFunction();
    std::cout << msg << std::endl;
}

void anotherCaller() {
    std::cout << middleFunction() << std::endl;
}
