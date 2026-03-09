// Call chain module for testing call hierarchy

pub fn leaf_function() -> String {
    String::from("leaf result")
}

pub fn middle_function() -> String {
    let result = leaf_function();
    format!("middle: {}", result)
}

pub fn entry_point() {
    let msg = middle_function();
    println!("{}", msg);
}

pub fn another_caller() {
    println!("{}", middle_function());
}
